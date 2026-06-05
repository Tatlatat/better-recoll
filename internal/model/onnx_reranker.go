package model

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/daulet/tokenizers"
	ort "github.com/yalue/onnxruntime_go"
)

const maxSeqLen = 256

type OnnxReranker struct {
	tokenizer *tokenizers.Tokenizer
	session   *ort.DynamicAdvancedSession
}

func NewOnnxReranker(modelPath, tokenizerPath string) (*OnnxReranker, error) {
	// Initialize onnxruntime environment if not done yet
	if !ort.IsInitialized() {
		lib := findOnnxRuntimeLib()
		if lib != "" {
			ort.SetSharedLibraryPath(lib)
		} else {
			ort.SetSharedLibraryPath("/opt/homebrew/lib/libonnxruntime.dylib")
		}
		err := ort.InitializeEnvironment()
		if err != nil {
			return nil, fmt.Errorf("failed to initialize onnxruntime environment: %w", err)
		}
	}

	// Load tokenizer
	tokenizerFile := filepath.Join(tokenizerPath, "tokenizer.json")
	tokenizer, err := tokenizers.FromFile(tokenizerFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load tokenizer from %s: %w", tokenizerFile, err)
	}

	// Create session (CPU — ổn định với external-data model; van RerankK lo tốc độ).
	session, err := ort.NewDynamicAdvancedSession(
		modelPath,
		[]string{"input_ids", "attention_mask"},
		[]string{"logits"},
		nil,
	)
	if err != nil {
		tokenizer.Close()
		return nil, fmt.Errorf("failed to create dynamic advanced session: %w", err)
	}

	return &OnnxReranker{
		tokenizer: tokenizer,
		session:   session,
	}, nil
}

func (r *OnnxReranker) Close() error {
	var errs []error
	if r.session != nil {
		err := r.session.Destroy()
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to destroy session: %w", err))
		}
	}
	if r.tokenizer != nil {
		r.tokenizer.Close()
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func (r *OnnxReranker) Score(query string, texts []string) ([]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	batchSize := int64(len(texts))

	// Tokenize query separately without special tokens
	queryIds, _, err := r.tokenizer.EncodeErr(query, false)
	if err != nil {
		return nil, fmt.Errorf("failed to tokenize query: %w", err)
	}

	// Tokenize each text separately and construct the sequence
	encodedIds := make([][]int64, len(texts))
	maxLen := 0

	for i, text := range texts {
		textIds, _, err := r.tokenizer.EncodeErr(text, false)
		if err != nil {
			return nil, fmt.Errorf("failed to tokenize text %q: %w", text, err)
		}

		// XLM-RoBERTa cross-encoder pair format: <s> query </s></s> text </s>
		// IDs mapping: bos = 0, eos = 2
		textLen := len(textIds)
		maxTextLen := maxSeqLen - len(queryIds) - 4
		if maxTextLen < 0 {
			maxTextLen = 0
		}
		if textLen > maxTextLen {
			textLen = maxTextLen
		}

		combined := make([]int64, 0, 1+len(queryIds)+2+textLen+1)
		combined = append(combined, 0)
		for _, id := range queryIds {
			combined = append(combined, int64(id))
		}
		combined = append(combined, 2, 2)
		for _, id := range textIds[:textLen] {
			combined = append(combined, int64(id))
		}
		combined = append(combined, 2)

		encodedIds[i] = combined
		if len(combined) > maxLen {
			maxLen = len(combined)
		}
	}

	if maxLen == 0 {
		return nil, errors.New("all query-text pairs are empty")
	}

	seqLen := int64(maxLen)

	// Prepare flat arrays for input tensors (int64)
	inputIDs := make([]int64, batchSize*seqLen)
	attentionMask := make([]int64, batchSize*seqLen)

	for i, ids := range encodedIds {
		for j := int64(0); j < seqLen; j++ {
			idx := int64(i)*seqLen + j
			if j < int64(len(ids)) {
				inputIDs[idx] = ids[j]
				attentionMask[idx] = 1
			} else {
				// Pad token is 1 for XLM-RoBERTa
				inputIDs[idx] = 1
				attentionMask[idx] = 0
			}
		}
	}

	// Create input tensors
	inputShape := ort.NewShape(batchSize, seqLen)
	inputIDsTensor, err := ort.NewTensor(inputShape, inputIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to create input_ids tensor: %w", err)
	}
	defer inputIDsTensor.Destroy()

	attentionMaskTensor, err := ort.NewTensor(inputShape, attentionMask)
	if err != nil {
		return nil, fmt.Errorf("failed to create attention_mask tensor: %w", err)
	}
	defer attentionMaskTensor.Destroy()

	// Run session
	inputs := []ort.Value{inputIDsTensor, attentionMaskTensor}
	outputs := []ort.Value{nil}

	err = r.session.Run(inputs, outputs)
	if err != nil {
		return nil, fmt.Errorf("failed to run session: %w", err)
	}
	defer outputs[0].Destroy()

	// Extract output tensor and data
	outputValue := outputs[0]
	outputTensor, ok := outputValue.(*ort.Tensor[float32])
	if !ok {
		return nil, fmt.Errorf("unexpected output value type: %T", outputValue)
	}

	outputShape := outputTensor.GetShape()
	if len(outputShape) != 2 || outputShape[1] != 1 {
		return nil, fmt.Errorf("expected output shape [batch, 1], got %v", outputShape)
	}

	outputData := outputTensor.GetData()

	scores := make([]float32, batchSize)
	for i := int64(0); i < batchSize; i++ {
		scores[i] = outputData[i]
	}

	return scores, nil
}

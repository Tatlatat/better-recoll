package model

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"

	"github.com/daulet/tokenizers"
	ort "github.com/yalue/onnxruntime_go"
)

type OnnxConfig struct {
	ModelPath     string
	TokenizerPath string
}

func DefaultOnnxConfig() OnnxConfig {
	return OnnxConfig{
		ModelPath:     "models/onnx/bge-m3/model.onnx",
		TokenizerPath: "models/onnx/bge-m3",
	}
}

type OnnxEmbedder struct {
	cfg       OnnxConfig
	tokenizer *tokenizers.Tokenizer
	session   *ort.DynamicAdvancedSession
}

func NewOnnxEmbedder(cfg OnnxConfig) (*OnnxEmbedder, error) {
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
	tokenizerFile := filepath.Join(cfg.TokenizerPath, "tokenizer.json")
	tokenizer, err := tokenizers.FromFile(tokenizerFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load tokenizer from %s: %w", tokenizerFile, err)
	}

	// Create session (CPU — ổn định với external-data model).
	session, err := ort.NewDynamicAdvancedSession(
		cfg.ModelPath,
		[]string{"input_ids", "attention_mask"},
		[]string{"token_embeddings"},
		nil,
	)
	if err != nil {
		tokenizer.Close()
		return nil, fmt.Errorf("failed to create dynamic advanced session: %w", err)
	}

	return &OnnxEmbedder{
		cfg:       cfg,
		tokenizer: tokenizer,
		session:   session,
	}, nil
}

func (e *OnnxEmbedder) Close() error {
	var errs []error
	if e.session != nil {
		err := e.session.Destroy()
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to destroy session: %w", err))
		}
	}
	if e.tokenizer != nil {
		e.tokenizer.Close()
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func (e *OnnxEmbedder) Embed(texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, errors.New("empty texts")
	}

	batchSize := int64(len(texts))
	encodedIds := make([][]uint32, len(texts))
	maxLen := 0

	for i, text := range texts {
		// Use EncodeWithOptions or Encode. Let's see if EncodeErr or Encode is available.
		// We'll write this and let the compiler tell us if there's any mismatch.
		ids, _, err := e.tokenizer.EncodeErr(text, true)
		if err != nil {
			return nil, fmt.Errorf("failed to tokenize text %q: %w", text, err)
		}
		encodedIds[i] = ids
		if len(ids) > maxLen {
			maxLen = len(ids)
		}
	}

	if maxLen == 0 {
		return nil, errors.New("all texts tokenized to empty sequences")
	}

	seqLen := int64(maxLen)

	// Prepare flat arrays for input tensors (int64)
	inputIDs := make([]int64, batchSize*seqLen)
	attentionMask := make([]int64, batchSize*seqLen)

	for i, ids := range encodedIds {
		for j := int64(0); j < seqLen; j++ {
			idx := int64(i)*seqLen + j
			if j < int64(len(ids)) {
				inputIDs[idx] = int64(ids[j])
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

	err = e.session.Run(inputs, outputs)
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
	if len(outputShape) != 3 {
		return nil, fmt.Errorf("expected 3D output shape, got %v", outputShape)
	}
	hiddenSize := outputShape[2] // should be 1024

	outputData := outputTensor.GetData()

	results := make([][]float32, batchSize)
	for i := int64(0); i < batchSize; i++ {
		startIdx := i * seqLen * hiddenSize
		clsVector := make([]float32, hiddenSize)
		copy(clsVector, outputData[startIdx:startIdx+hiddenSize])

		// L2 normalize
		var sumSq float64
		for _, val := range clsVector {
			sumSq += float64(val * val)
		}
		norm := math.Sqrt(sumSq)
		if norm > 1e-12 {
			for j := range clsVector {
				clsVector[j] = float32(float64(clsVector[j]) / norm)
			}
		}

		results[i] = clsVector
	}

	return results, nil
}

func (e *OnnxEmbedder) Rerank(query string, texts []string) ([]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	queryEmbs, err := e.Embed([]string{query})
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}
	queryEmb := queryEmbs[0]

	textEmbs, err := e.Embed(texts)
	if err != nil {
		return nil, fmt.Errorf("failed to embed texts: %w", err)
	}

	scores := make([]float32, len(texts))
	for i, emb := range textEmbs {
		var dot float32
		for j := range emb {
			dot += queryEmb[j] * emb[j]
		}
		scores[i] = dot
	}

	return scores, nil
}

func (e *OnnxEmbedder) Dim() int {
	return 1024
}

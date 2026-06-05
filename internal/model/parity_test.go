package model

// Parity oracle — viết bởi Claude (orchestrator), KHÔNG để worker sửa.
// Cổng sinh-tử Wave 0: vector của OnnxEmbedder (Go) phải khớp vector
// tham chiếu sinh từ sentence-transformers (Python) tới cosine > 0.9999.
// Lệch = tokenizer/pooling/normalize trong Go sai -> recall hỏng âm thầm.

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

const (
	refVectorsPath = "../../scripts/parity/reference_vectors.npy"
	sampleTexts    = "../../scripts/parity/sample_texts.json"
	parityFloor    = 0.9999
)

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func TestParityWithPython(t *testing.T) {
	if _, err := os.Stat(refVectorsPath); err != nil {
		t.Skipf("reference vectors chưa có (%v) — chạy scripts/parity/reference_vectors.py trước", err)
	}
	ref, err := loadNpyFloat32(refVectorsPath)
	if err != nil {
		t.Fatalf("load npy: %v", err)
	}
	raw, err := os.ReadFile(sampleTexts)
	if err != nil {
		t.Fatalf("read sample_texts: %v", err)
	}
	var texts []string
	if err := json.Unmarshal(raw, &texts); err != nil {
		t.Fatalf("parse sample_texts: %v", err)
	}
	if len(texts) != len(ref) {
		t.Fatalf("mismatch: %d texts vs %d ref vectors", len(texts), len(ref))
	}

	// Test chạy với cwd = internal/model/, nên giải path tương đối từ DefaultOnnxConfig
	// (trỏ từ repo root) sang absolute để tìm đúng model dir.
	cfg := DefaultOnnxConfig()
	root, _ := filepath.Abs("../..")
	cfg.ModelPath = filepath.Join(root, cfg.ModelPath)
	cfg.TokenizerPath = filepath.Join(root, cfg.TokenizerPath)

	emb, err := NewOnnxEmbedder(cfg)
	if err != nil {
		t.Fatalf("NewOnnxEmbedder: %v", err)
	}
	defer emb.Close()

	got, err := emb.Embed(texts)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != len(ref) {
		t.Fatalf("Embed returned %d vectors, want %d", len(got), len(ref))
	}

	var worst float64 = 1.0
	for i := range texts {
		c := cosine(got[i], ref[i])
		if c < worst {
			worst = c
		}
		if c < parityFloor {
			t.Errorf("text[%d]=%q cosine=%.6f < %.4f (Go vector lệch Python)", i, texts[i], c, parityFloor)
		}
	}
	t.Logf("PARITY: worst cosine = %.6f over %d texts (floor %.4f)", worst, len(texts), parityFloor)
}

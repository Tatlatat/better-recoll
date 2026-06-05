package model

// Reranker oracle — Claude viết. Cross-encoder thật phải đạt recall@1 cao
// hơn hẳn bi-encoder (Python cross-encoder cho 1.00 trên cùng bộ pairs).
// Đây là cổng survival ĐÚNG: dùng cross-encoder, không phải cosine embedding.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRerankerSurvival(t *testing.T) {
	root, _ := filepath.Abs("../..")
	raw, err := os.ReadFile(filepath.Join(root, "testdata/pairs.json"))
	if err != nil {
		t.Fatalf("read pairs.json: %v", err)
	}
	var pairs []pair
	if err := json.Unmarshal(raw, &pairs); err != nil {
		t.Fatalf("parse: %v", err)
	}

	rr, err := NewOnnxReranker(
		filepath.Join(root, "models/onnx/bge-reranker/model.onnx"),
		filepath.Join(root, "models/onnx/bge-reranker"),
	)
	if err != nil {
		t.Fatalf("NewOnnxReranker: %v", err)
	}
	defer rr.Close()

	hits := 0
	var total time.Duration
	for _, p := range pairs {
		cands := append([]string{p.Positive}, p.HardNegatives...)
		start := time.Now()
		scores, err := rr.Score(p.Query, cands)
		total += time.Since(start)
		if err != nil {
			t.Fatalf("Score %q: %v", p.Query, err)
		}
		best := 0
		for i, s := range scores {
			if s > scores[best] {
				best = i
			}
		}
		if best == 0 {
			hits++
		} else {
			t.Logf("MISS query=%q: positive sau candidate[%d] (scores=%v)", p.Query, best, scores)
		}
	}
	recall := float64(hits) / float64(len(pairs))
	avgMs := float64(total.Milliseconds()) / float64(len(pairs))
	t.Logf("RERANKER recall@1 = %.2f (%d/%d), avg %.0f ms/query", recall, hits, len(pairs), avgMs)
	if recall < 0.85 {
		t.Errorf("cross-encoder recall@1 %.2f < 0.85", recall)
	}
}

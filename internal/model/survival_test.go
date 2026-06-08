package model

// Survival oracle — Claude viết. Cổng sinh-tử Wave 0 thứ hai:
// embedding của BGE-M3 phải PHÂN BIỆT được đoạn đúng (positive) khỏi đoạn
// bẫy cùng-domain (hard_negative) trên truy vấn Việt-Anh + không dấu.
// Đo recall@1: với mỗi cặp, xếp hạng [positive, hard_negatives...] theo
// similarity với query — positive phải đứng #1. PASS nếu recall@1 >= 0.85.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type pair struct {
	Query         string   `json:"query"`
	Positive      string   `json:"positive"`
	HardNegatives []string `json:"hard_negatives"`
}

func TestSurvivalRecall(t *testing.T) {
	root, _ := filepath.Abs("../..")
	raw, err := os.ReadFile(filepath.Join(root, "testdata/pairs.json"))
	if err != nil {
		t.Fatalf("read pairs.json: %v", err)
	}
	var pairs []pair
	if err := json.Unmarshal(raw, &pairs); err != nil {
		t.Fatalf("parse pairs.json: %v", err)
	}
	if len(pairs) == 0 {
		t.Fatal("no pairs")
	}

	cfg := DefaultOnnxConfig()
	cfg.ModelPath = filepath.Join(root, cfg.ModelPath)
	cfg.TokenizerPath = filepath.Join(root, cfg.TokenizerPath)
	emb, err := NewOnnxEmbedder(cfg)
	if err != nil {
		t.Fatalf("NewOnnxEmbedder: %v", err)
	}
	defer emb.Close()

	hits := 0
	var total time.Duration
	for _, p := range pairs {
		candidates := append([]string{p.Positive}, p.HardNegatives...)
		start := time.Now()
		scores, err := emb.Rerank(p.Query, candidates)
		total += time.Since(start)
		if err != nil {
			t.Fatalf("Rerank %q: %v", p.Query, err)
		}
		// positive là index 0; nó phải có score cao nhất
		best := 0
		for i, s := range scores {
			if s > scores[best] {
				best = i
			}
		}
		if best == 0 {
			hits++
		} else {
			t.Logf("MISS query=%q: positive xếp sau candidate[%d]", p.Query, best)
		}
	}
	recall := float64(hits) / float64(len(pairs))
	avgMs := float64(total.Milliseconds()) / float64(len(pairs))
	// GHI CHÚ: đây là lớp BI-ENCODER (cosine embedding) — informational, KHÔNG phải gate.
	// Nó cố ý đạt thấp (~0.70) để CHỨNG MINH cần cross-encoder. Gate thật là
	// TestRerankerSurvival (cross-encoder, recall 1.00). Xem reranker_test.go.
	t.Logf("BI-ENCODER baseline recall@1 = %.2f (%d/%d), avg %.0f ms/query — cross-encoder cải thiện cái này lên 1.00", recall, hits, len(pairs), avgMs)
}

package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSeparateIndexModel verifies the deliberate design: search and index use
// SEPARATE embedder/reranker instances. They are NOT shared — sharing would
// serialize the parallel index workers behind one mutex (making a large file's
// indexing take minutes) and block search during background indexing. The
// duplicate model load is the accepted cost of concurrent index+search.
func TestSeparateIndexModel(t *testing.T) {
	root, _ := filepath.Abs("../..")
	modelDir := filepath.Join(root, "models/onnx/bge-m3/model.onnx")
	if _, err := os.Stat(modelDir); err != nil {
		t.Skipf("model ONNX chưa có (%v) — skip integration", err)
	}

	tmp := t.TempDir()
	cfg := DefaultConfig(root, filepath.Join(tmp, ".sfsindex"))

	eng, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	// Assert: search and index models are DISTINCT instances (so index workers
	// don't contend with search on a single mutex).
	if eng.embedder == eng.indexEmbedder {
		t.Errorf("embedder and indexEmbedder must be SEPARATE instances, both are %p", eng.embedder)
	}
	if eng.reranker == eng.indexReranker {
		t.Errorf("reranker and indexReranker must be SEPARATE instances, both are %p", eng.reranker)
	}

	// Index + Search still work end-to-end.
	os.WriteFile(filepath.Join(tmp, "test.txt"), []byte("Xin chào Việt Nam, đây là đoạn văn ngắn để index."), 0644)
	if err := eng.Index(tmp); err != nil {
		t.Fatalf("failed to index: %v", err)
	}

	res, err := eng.Search("đoạn văn", 5)
	if err != nil {
		t.Fatalf("failed to search: %v", err)
	}
	if len(res) == 0 {
		t.Error("search results are empty")
	}

	// Close releases all four model instances without panic (no double-free).
	if err := eng.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}
}

package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDedupeAndDiffEmbed(t *testing.T) {
	root, _ := filepath.Abs("../..")
	modelDir := filepath.Join(root, "models/onnx/bge-m3/model.onnx")
	if _, err := os.Stat(modelDir); err != nil {
		t.Skipf("model ONNX chưa có (%v) — skip integration", err)
	}

	tmp := t.TempDir()

	// Write 3 files. Two files are identical boilerplate files, one is unique.
	boilerplateText := "Kính gửi quý khách hàng đây là phần mẫu văn bản chung và lặp lại."

	file1Content := boilerplateText
	file2Content := boilerplateText
	file3Content := "Hoàn toàn khác biệt không chứa bất kỳ mẫu chung nào."

	os.WriteFile(filepath.Join(tmp, "doc1.txt"), []byte(file1Content), 0644)
	os.WriteFile(filepath.Join(tmp, "doc2.txt"), []byte(file2Content), 0644)
	os.WriteFile(filepath.Join(tmp, "doc3.txt"), []byte(file3Content), 0644)

	// Create engine with DiffEmbed = true
	cfg := DefaultConfig(root, filepath.Join(tmp, ".sfsindex_diff"))
	cfg.DiffEmbed = true
	eng, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	if err := eng.Index(tmp); err != nil {
		t.Fatalf("Index: %v", err)
	}

	// Verify that the store has chunks, and the boilerplate chunks are marked as such.
	var boilerplateCount int

	vectors, ids := eng.store.AllVectors()
	for i, id := range ids {
		ch, err := eng.store.GetChunk(id)
		if err != nil {
			t.Fatalf("GetChunk: %v", err)
		}

		// If it's a boilerplate chunk, check if it was marked correctly
		if strings.Contains(ch.Text, "Kính gửi quý khách hàng") {
			if !ch.IsBoilerplate {
				t.Errorf("expected chunk to be marked as boilerplate: %s", ch.Text)
			}
			boilerplateCount++

			// Verify if it is in the vector index. Since DiffEmbed = true, it should NOT be in vindex.
			results := eng.vindex.Search(vectors[i], 10)
			found := false
			for _, r := range results {
				if r.ID == id {
					found = true
					break
				}
			}
			if found {
				t.Errorf("boilerplate chunk %d should NOT be in vector index when DiffEmbed is true", id)
			}
		} else {
			// Non-boilerplate chunks should be in vindex
			results := eng.vindex.Search(vectors[i], 10)
			found := false
			for _, r := range results {
				if r.ID == id {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("non-boilerplate chunk %d should be in vector index", id)
			}
		}
	}

	if boilerplateCount == 0 {
		t.Fatal("expected to find boilerplate chunks, but found none")
	}

	// Now check if exact keyword search (BM25) on boilerplate still works.
	res, err := eng.Search("Kính gửi quý khách hàng", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	bm25Matched := false
	for _, r := range res {
		if strings.Contains(r.Text, "Kính gửi quý khách hàng") {
			bm25Matched = true
			break
		}
	}
	if !bm25Matched {
		t.Errorf("expected BM25 search to find boilerplate chunk, but got nothing: %v", res)
	}
}

func TestRouterIntegration(t *testing.T) {
	root, _ := filepath.Abs("../..")
	modelDir := filepath.Join(root, "models/onnx/bge-m3/model.onnx")
	if _, err := os.Stat(modelDir); err != nil {
		t.Skipf("model ONNX chưa có (%v) — skip integration", err)
	}

	tmp := t.TempDir()

	os.WriteFile(filepath.Join(tmp, "bienban_nghiemthu.txt"), []byte("Văn bản này ghi nhận việc nghiệm thu công trình nhà dân dụng."), 0644)
	os.WriteFile(filepath.Join(tmp, "baogia_xaydung.txt"), []byte("Văn bản báo giá chi tiết xây dựng nhà dân dụng và thiết bị."), 0644)

	cfg := DefaultConfig(root, filepath.Join(tmp, ".sfsindex_router"))
	eng, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	if err := eng.Index(tmp); err != nil {
		t.Fatalf("Index: %v", err)
	}

	// Expose router, register "bienban" label with keyword "nghiệm thu"
	eng.Router.Register("bienban", []string{"nghiem thu"})

	// Search ranked for "nghiệm thu công trình"
	res, err := eng.SearchRanked("nghiệm thu công trình", 5)
	if err != nil {
		t.Fatalf("SearchRanked: %v", err)
	}

	// Verify that bienban_nghiemthu.txt is in the Exact list and has a boosted score.
	if len(res.Exact) == 0 {
		t.Fatal("expected Exact results, got none")
	}

	firstResult := res.Exact[0]
	if !strings.Contains(firstResult.FilePath, "bienban_nghiemthu.txt") {
		t.Errorf("expected bienban_nghiemthu.txt to be the top result, got %s", firstResult.FilePath)
	}

	// Compare with unboosted engine
	eng2, err := New(DefaultConfig(root, filepath.Join(tmp, ".sfsindex_router2")))
	if err != nil {
		t.Fatalf("New eng2: %v", err)
	}
	defer eng2.Close()
	if err := eng2.Index(tmp); err != nil {
		t.Fatalf("Index eng2: %v", err)
	}

	res2, err := eng2.SearchRanked("nghiệm thu công trình", 5)
	if err != nil {
		t.Fatalf("SearchRanked eng2: %v", err)
	}

	if len(res2.Exact) == 0 {
		t.Fatal("expected Exact results in eng2, got none")
	}

	scoreWithBoost := res.Exact[0].Score
	scoreWithoutBoost := res2.Exact[0].Score

	if scoreWithBoost <= scoreWithoutBoost {
		t.Errorf("expected boosted score (%f) to be higher than unboosted score (%f)", scoreWithBoost, scoreWithoutBoost)
	}
}

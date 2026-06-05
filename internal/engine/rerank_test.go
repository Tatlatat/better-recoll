package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// Oracle Claude. SearchRanked dùng cross-encoder rerank (VAN) rồi chia 2 ô:
// Exact (trên ngưỡng) / Suggest (dưới). File đúng phải vào Exact và đứng đầu.
func TestSearchRankedTwoBuckets(t *testing.T) {
	root, _ := filepath.Abs("../..")
	if _, err := os.Stat(filepath.Join(root, "models/onnx/bge-reranker/model.onnx")); err != nil {
		t.Skipf("reranker ONNX chưa có: %v", err)
	}

	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "nghiemthu.txt"),
		[]byte("BIÊN BẢN NGHIỆM THU hoàn thành hạng mục công trình xây dựng nhà xưởng"), 0644)
	os.WriteFile(filepath.Join(tmp, "baogia.txt"),
		[]byte("BẢNG BÁO GIÁ VẬT TƯ xi măng sắt thép cát đá cho công trình"), 0644)
	os.WriteFile(filepath.Join(tmp, "hopdong.txt"),
		[]byte("HỢP ĐỒNG THI CÔNG xây dựng nhà xưởng giữa bên A và bên B"), 0644)

	eng, err := New(DefaultConfig(root, filepath.Join(tmp, ".sfsindex")))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()
	if err := eng.Index(tmp); err != nil {
		t.Fatalf("Index: %v", err)
	}

	res, err := eng.SearchRanked("biên bản nghiệm thu công trình", 10)
	if err != nil {
		t.Fatalf("SearchRanked: %v", err)
	}
	if len(res.Exact) == 0 {
		t.Fatal("ô Exact rỗng — file đúng phải vào đây")
	}
	if filepath.Base(res.Exact[0].FilePath) != "nghiemthu.txt" {
		t.Errorf("Exact[0] = %s, want nghiemthu.txt", filepath.Base(res.Exact[0].FilePath))
	}
	// hopdong.txt (bẫy, cùng "nhà xưởng") KHÔNG được đứng đầu Exact
	for _, r := range res.Exact {
		if filepath.Base(r.FilePath) == "nghiemthu.txt" {
			return // đúng file có trong Exact -> pass
		}
	}
	t.Error("nghiemthu.txt không có trong ô Exact")
}

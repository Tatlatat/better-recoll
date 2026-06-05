package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// Oracle Claude — integration. Index thư mục file mẫu rồi Search phải trả
// đúng file chứa nội dung ở top. Đây là chứng minh end-to-end baseline.
// Cần model ONNX (skip nếu chưa có, để CI nhẹ không kẹt).
func TestEngineIndexAndSearch(t *testing.T) {
	root, _ := filepath.Abs("../..")
	modelDir := filepath.Join(root, "models/onnx/bge-m3/model.onnx")
	if _, err := os.Stat(modelDir); err != nil {
		t.Skipf("model ONNX chưa có (%v) — skip integration", err)
	}

	tmp := t.TempDir()
	// 2 file văn bản tiếng Việt khác chủ đề
	os.WriteFile(filepath.Join(tmp, "nghiemthu.txt"),
		[]byte("BIÊN BẢN NGHIỆM THU hoàn thành hạng mục công trình xây dựng nhà xưởng"), 0644)
	os.WriteFile(filepath.Join(tmp, "baogia.txt"),
		[]byte("BẢNG BÁO GIÁ VẬT TƯ xi măng sắt thép cát đá cho công trình"), 0644)

	eng, err := New(DefaultConfig(root, filepath.Join(tmp, ".sfsindex")))
	if err != nil {
		t.Fatalf("New engine: %v", err)
	}
	defer eng.Close()

	if err := eng.Index(tmp); err != nil {
		t.Fatalf("Index: %v", err)
	}

	res, err := eng.Search("nghiệm thu công trình", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) == 0 {
		t.Fatal("Search trả rỗng")
	}
	// File nghiemthu.txt phải đứng đầu
	if filepath.Base(res[0].FilePath) != "nghiemthu.txt" {
		t.Errorf("top result = %s, want nghiemthu.txt", filepath.Base(res[0].FilePath))
	}
}

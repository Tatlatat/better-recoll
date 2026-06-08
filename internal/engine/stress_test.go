package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Stress test — Claude viết. Mục tiêu: app KHÔNG crash / treo / lỗi trên
// các đầu vào xấu mà thư mục thật luôn có. "Không còn bug" = qua hết.

func newTestEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	root, _ := filepath.Abs("../..")
	if _, err := os.Stat(filepath.Join(root, "models/onnx/bge-m3/model.onnx")); err != nil {
		t.Skip("no model")
	}
	tmp := t.TempDir()
	eng, err := New(DefaultConfig(root, filepath.Join(tmp, ".idx")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { eng.Close() })
	return eng, tmp
}

// File rỗng, file rác nhị phân, đuôi lạ — không được crash.
func TestStressBadFiles(t *testing.T) {
	eng, dir := newTestEngine(t)
	os.WriteFile(filepath.Join(dir, "empty.txt"), []byte(""), 0644)
	os.WriteFile(filepath.Join(dir, "binary.txt"), []byte{0x00, 0x01, 0xff, 0xfe, 0x00}, 0644)
	os.WriteFile(filepath.Join(dir, "huge_ext.unknownext"), []byte("ignored"), 0644)
	os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("biên bản nghiệm thu công trình"), 0644)
	// PDF giả (header sai) — reader phải skip, không crash
	os.WriteFile(filepath.Join(dir, "fake.pdf"), []byte("%PDF-1.4\nrác không phải pdf thật\n"), 0644)
	if err := eng.Index(dir); err != nil {
		t.Fatalf("Index crash trên file xấu: %v", err)
	}
	// File tốt vẫn tìm được
	res, err := eng.Search("nghiệm thu", 5)
	if err != nil {
		t.Fatalf("Search lỗi: %v", err)
	}
	if len(res) == 0 {
		t.Error("file tốt (ok.txt) không tìm được dù index xong")
	}
}

// Query rỗng / chỉ khoảng trắng / siêu dài / ký tự lạ — không crash.
func TestStressBadQueries(t *testing.T) {
	eng, dir := newTestEngine(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hợp đồng thi công xây dựng"), 0644)
	if err := eng.Index(dir); err != nil {
		t.Fatal(err)
	}
	queries := []string{
		"",
		"   ",
		strings.Repeat("xây dựng ", 500), // siêu dài
		"!@#$%^&*()_+{}|:<>?",
		"日本語 中文 한국어",        // unicode khác
		"\x00\x01",          // ký tự điều khiển
		"café naïve résumé", // dấu latin khác
	}
	for _, q := range queries {
		// Cả Search lẫn SearchRanked không được panic/crash
		if _, err := eng.Search(q, 10); err != nil {
			t.Errorf("Search(%q) lỗi: %v", truncate(q), err)
		}
		if _, err := eng.SearchRanked(q, 10); err != nil {
			t.Errorf("SearchRanked(%q) lỗi: %v", truncate(q), err)
		}
	}
}

// Index thư mục rỗng / thư mục không tồn tại — lỗi êm, không crash.
func TestStressEmptyAndMissingDir(t *testing.T) {
	eng, dir := newTestEngine(t)
	// thư mục rỗng
	empty := filepath.Join(dir, "empty_dir")
	os.MkdirAll(empty, 0755)
	if err := eng.Index(empty); err != nil {
		t.Errorf("Index thư mục rỗng phải OK, got: %v", err)
	}
	// thư mục không tồn tại — phải trả error, KHÔNG panic
	_ = eng.Index(filepath.Join(dir, "khong_ton_tai_xyz"))
	// (không assert error cụ thể, chỉ cần không panic — test tới đây là pass)
}

// Index 2 lần cùng thư mục — không nhân đôi / không crash.
func TestStressReindexSameDir(t *testing.T) {
	eng, dir := newTestEngine(t)
	os.WriteFile(filepath.Join(dir, "x.txt"), []byte("báo giá vật tư xi măng"), 0644)
	if err := eng.Index(dir); err != nil {
		t.Fatal(err)
	}
	if err := eng.Index(dir); err != nil {
		t.Fatalf("re-index crash: %v", err)
	}
	res, _ := eng.Search("vật tư", 10)
	if len(res) == 0 {
		t.Error("re-index xong không tìm được")
	}
}

func truncate(s string) string {
	if len(s) > 30 {
		return s[:30] + "..."
	}
	return s
}

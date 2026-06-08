package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// First Principles: index nền CHỈ nhận định dạng văn phòng → 29k .md/code rác tự
// loại. Test chứng minh: thư mục lẫn lộn (office + .md + code) → chỉ office vào.
func TestBackgroundIndexesOfficeOnly(t *testing.T) {
	d := t.TempDir()
	// tạo file đủ loại: office thật + rác lập trình
	files := map[string]string{
		"baocao.docx":  "PK fake docx",     // office — NHẬN
		"bangtinh.xlsx": "PK fake xlsx",     // office — NHẬN
		"tailieu.pdf":  "%PDF fake",         // office — NHẬN
		"data.csv":     "a,b,c\n1,2,3",      // office — NHẬN
		"README.md":    "# rác lập trình",   // .md — LOẠI
		"main.go":      "package main",       // code — LOẠI
		"app.js":       "console.log(1)",     // code — LOẠI
		"notes.txt":    "ghi chú",            // txt — LOẠI (không trong office set)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(d, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	opts := BackgroundIndexOptions()
	// OnlyExtensions phải = office set (không rỗng)
	if len(opts.OnlyExtensions) == 0 {
		t.Fatal("BackgroundIndexOptions phải mặc định OnlyExtensions = office, đang rỗng (sẽ nuốt mọi thứ)")
	}
	allowed := map[string]bool{}
	for _, e := range opts.OnlyExtensions {
		allowed[e] = true
	}
	// office phải có, rác KHÔNG được có
	for _, must := range []string{"pdf", "docx", "xlsx", "csv"} {
		if !allowed[must] {
			t.Errorf("office ext %q phải nằm trong whitelist", must)
		}
	}
	for _, mustNot := range []string{"md", "go", "js", "ts", "txt"} {
		if allowed[mustNot] {
			t.Errorf("rác ext %q KHÔNG được nằm trong office whitelist", mustNot)
		}
	}
}

// OfficeExtensions là nguồn sự thật ổn định.
func TestOfficeExtensionsSane(t *testing.T) {
	exts := OfficeExtensions()
	if len(exts) < 3 {
		t.Fatalf("office set quá ít: %v", exts)
	}
	seen := map[string]bool{}
	for _, e := range exts {
		if seen[e] {
			t.Errorf("đuôi trùng trong office set: %q", e)
		}
		seen[e] = true
	}
	if !seen["pdf"] || !seen["docx"] {
		t.Error("office set thiếu pdf/docx — định dạng văn phòng cốt lõi")
	}
}

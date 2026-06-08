package reader

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tạo .pptx tối thiểu (ZIP + 1 slide XML có text) để test không phụ thuộc file ngoài.
func writeMinimalPptx(t *testing.T, path string, slideText string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	slideXML := `<?xml version="1.0"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
  <p:cSld><p:spTree>
    <a:p><a:r><a:t>` + slideText + `</a:t></a:r></a:p>
  </p:spTree></p:cSld>
</p:sld>`
	w, err := zw.Create("ppt/slides/slide1.xml")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte(slideXML))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPptxReadsSlideText(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "baigiang.pptx")
	writeMinimalPptx(t, p, "Quản trị chi phí Lecture 10")

	// .pptx phải được registry nhận
	if _, ok := Registry[".pptx"]; !ok {
		t.Fatal(".pptx chưa được đăng ký trong Registry")
	}

	got, err := ReadFile(p)
	if err != nil {
		t.Fatalf("đọc .pptx lỗi: %v", err)
	}
	if !strings.Contains(got, "Quản trị chi phí Lecture 10") {
		t.Fatalf("không trích được text slide, got %q", got)
	}
}

package reader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRtfStripsControlWords(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "tailieu.rtf")
	// RTF thật: header + font table (phải bỏ) + nội dung
	rtf := `{\rtf1\ansi\deff0{\fonttbl{\f0 Times;}}\f0\fs24 Bao cao tai chinh quy 4\par Doanh thu tang 20 phan tram\par}`
	if err := os.WriteFile(p, []byte(rtf), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := Registry[".rtf"]; !ok {
		t.Fatal(".rtf chưa đăng ký")
	}
	got, err := ReadFile(p)
	if err != nil {
		t.Fatalf("đọc rtf lỗi: %v", err)
	}
	if !strings.Contains(got, "Bao cao tai chinh") {
		t.Errorf("không trích được nội dung: %q", got)
	}
	if !strings.Contains(got, "Doanh thu tang 20 phan tram") {
		t.Errorf("mất đoạn 2: %q", got)
	}
	// KHÔNG được lẫn tên font / control word
	if strings.Contains(got, "Times") || strings.Contains(got, "fonttbl") || strings.Contains(got, `\f0`) {
		t.Errorf("lẫn metadata/control word: %q", got)
	}
}

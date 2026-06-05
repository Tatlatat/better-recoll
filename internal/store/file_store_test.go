package store

import (
	"os"
	"path/filepath"
	"testing"
)

// Oracle Claude. FileStore: ghi chunk ra đĩa, đọc lại đúng vector + text.
func TestFileStoreRoundtrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFileStore(filepath.Join(dir, "idx"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	chunks := []Chunk{
		{ID: 1, FilePath: "a.docx", Text: "Biên bản nghiệm thu", NormText: "bien ban nghiem thu", Offset: 0, Vector: []float32{0.1, 0.2, 0.3}},
		{ID: 2, FilePath: "b.xlsx", Text: "Báo giá vật tư", NormText: "bao gia vat tu", Offset: 10, Vector: []float32{0.4, 0.5, 0.6}},
	}
	if err := s.Write(chunks); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if s.Count() != 2 {
		t.Errorf("Count = %d, want 2", s.Count())
	}
	vecs, ids := s.AllVectors()
	if len(vecs) != 2 || len(ids) != 2 {
		t.Fatalf("AllVectors len = %d/%d, want 2/2", len(vecs), len(ids))
	}
	c, err := s.GetChunk(2)
	if err != nil {
		t.Fatalf("GetChunk(2): %v", err)
	}
	if c.Text != "Báo giá vật tư" || c.FilePath != "b.xlsx" {
		t.Errorf("GetChunk(2) = %+v, sai text/path", c)
	}
	s.Close()

	// Mở lại từ đĩa — dữ liệu phải còn (persistence)
	s2, err := NewFileStore(filepath.Join(dir, "idx"))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if s2.Count() != 2 {
		t.Errorf("sau reopen Count = %d, want 2 (không persist?)", s2.Count())
	}
	if _, err := os.Stat(filepath.Join(dir, "idx")); err != nil {
		t.Errorf("index dir không tồn tại: %v", err)
	}
}

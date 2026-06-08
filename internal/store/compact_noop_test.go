package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompactNoopDoesNotRewrite(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "idx")
	s, err := NewFileStore(indexPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	chunks := []Chunk{
		{ID: 10, FilePath: "a.docx", Text: "Biên bản nghiệm thu", NormText: "bien ban nghiem thu", Offset: 0, Vector: []float32{0.1, 0.2}},
		{ID: 20, FilePath: "b.xlsx", Text: "Báo giá vật tư", NormText: "bao gia vat tu", Offset: 10, Vector: []float32{0.3, 0.4}},
	}
	if err := s.Write(chunks); err != nil {
		t.Fatalf("Write: %v", err)
	}

	s.AddIndexedDir("some/clean/dir")

	gobPath := filepath.Join(indexPath, "chunks.gob")
	dirsPath := filepath.Join(indexPath, "dirs.json")

	infoGob, err := os.Stat(gobPath)
	if err != nil {
		t.Fatalf("stat chunks.gob: %v", err)
	}
	infoDirs, err := os.Stat(dirsPath)
	if err != nil {
		t.Fatalf("stat dirs.json: %v", err)
	}

	mtimeGob1 := infoGob.ModTime()
	mtimeDirs1 := infoDirs.ModTime()

	// Sleep a bit to make sure mtime would differ if written
	time.Sleep(50 * time.Millisecond)

	// Compact with keep-all predicate
	kept, err := s.Compact(
		func(c Chunk) bool { return true },
		func(d string) bool { return true },
	)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}

	if len(kept) != 2 {
		t.Errorf("expected 2 chunks kept, got %d", len(kept))
	}

	// Assert chunk IDs did NOT change
	if kept[0].ID != 10 || kept[1].ID != 20 {
		t.Errorf("expected chunk IDs to be preserved (10, 20), got (%d, %d)", kept[0].ID, kept[1].ID)
	}

	infoGob2, err := os.Stat(gobPath)
	if err != nil {
		t.Fatalf("stat chunks.gob second time: %v", err)
	}
	infoDirs2, err := os.Stat(dirsPath)
	if err != nil {
		t.Fatalf("stat dirs.json second time: %v", err)
	}

	if !infoGob2.ModTime().Equal(mtimeGob1) {
		t.Errorf("chunks.gob mtime changed; was rewritten unnecessarily")
	}
	if !infoDirs2.ModTime().Equal(mtimeDirs1) {
		t.Errorf("dirs.json mtime changed; was rewritten unnecessarily")
	}

	s.Close()
}

func TestCompactStillRemovesJunk(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "idx")
	s, err := NewFileStore(indexPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	chunks := []Chunk{
		{ID: 10, FilePath: "a.docx", Text: "Biên bản nghiệm thu", NormText: "bien ban nghiem thu", Offset: 0, Vector: []float32{0.1, 0.2}},
		{ID: 20, FilePath: "node_modules/junk.txt", Text: "junk text", NormText: "junk text", Offset: 10, Vector: []float32{0.3, 0.4}},
	}
	if err := s.Write(chunks); err != nil {
		t.Fatalf("Write: %v", err)
	}

	s.AddIndexedDir("some/clean/dir")
	s.AddIndexedDir("some/node_modules/dir")

	gobPath := filepath.Join(indexPath, "chunks.gob")
	dirsPath := filepath.Join(indexPath, "dirs.json")

	infoGob, err := os.Stat(gobPath)
	if err != nil {
		t.Fatalf("stat chunks.gob: %v", err)
	}
	infoDirs, err := os.Stat(dirsPath)
	if err != nil {
		t.Fatalf("stat dirs.json: %v", err)
	}

	mtimeGob1 := infoGob.ModTime()
	mtimeDirs1 := infoDirs.ModTime()

	// Sleep a bit to make sure mtime changes if rewritten
	time.Sleep(50 * time.Millisecond)

	// Compact with keep-no-junk predicate
	kept, err := s.Compact(
		func(c Chunk) bool { return !strings.Contains(c.FilePath, "node_modules") },
		func(d string) bool { return !strings.Contains(d, "node_modules") },
	)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}

	if len(kept) != 1 {
		t.Errorf("expected 1 chunk kept, got %d", len(kept))
	}

	// Assert chunk IDs did change (reassigned starting from 0)
	if kept[0].ID != 0 {
		t.Errorf("expected kept chunk ID to be reassigned to 0, got %d", kept[0].ID)
	}

	infoGob2, err := os.Stat(gobPath)
	if err != nil {
		t.Fatalf("stat chunks.gob: %v", err)
	}
	infoDirs2, err := os.Stat(dirsPath)
	if err != nil {
		t.Fatalf("stat dirs.json: %v", err)
	}

	if infoGob2.ModTime().Equal(mtimeGob1) {
		t.Errorf("expected chunks.gob mtime to change (rewritten)")
	}
	if infoDirs2.ModTime().Equal(mtimeDirs1) {
		t.Errorf("expected dirs.json mtime to change (rewritten)")
	}

	s.Close()
}

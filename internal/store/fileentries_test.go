package store

import (
	"testing"
)

func TestFileEntriesGroupsByPath(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 2 chunk cùng file A, 1 chunk file B
	fs.Write([]Chunk{
		{ID: 1, FilePath: "/a.txt", Vector: []float32{1, 0}, ModTime: 100},
		{ID: 2, FilePath: "/a.txt", Vector: []float32{0, 1}, ModTime: 100},
		{ID: 3, FilePath: "/b.txt", Vector: []float32{1, 1}, ModTime: 200},
	})

	entries := fs.FileEntries()
	if len(entries) != 2 {
		t.Fatalf("muốn 2 file, được %d", len(entries))
	}
	// tìm file A
	var a *FileEntry
	for i := range entries {
		if entries[i].Path == "/a.txt" {
			a = &entries[i]
		}
	}
	if a == nil {
		t.Fatal("không thấy /a.txt")
	}
	// vector trung bình của A = ([1,0]+[0,1])/2 = [0.5,0.5]
	if a.Vector[0] != 0.5 || a.Vector[1] != 0.5 {
		t.Fatalf("vector TB sai: %v", a.Vector)
	}
	if a.ModTime != 100 {
		t.Fatalf("mtime sai: %d", a.ModTime)
	}
}

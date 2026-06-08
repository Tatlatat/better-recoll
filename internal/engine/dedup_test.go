package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// Re-indexing the same directory must NOT create duplicate chunks.
// This is the regression test for the "index nhân bản" bug where a file
// got indexed 6x (74% of the index was duplicate chunks), polluting the
// candidate pool so the right file never surfaced.
func TestReindexNoDuplicates(t *testing.T) {
	root, _ := filepath.Abs("../..")
	if _, err := os.Stat(filepath.Join(root, "models/onnx/bge-m3/model.onnx")); err != nil {
		t.Skip("no model")
	}

	d := t.TempDir()
	for i := 0; i < 5; i++ {
		os.WriteFile(filepath.Join(d, "doc"+string(rune('a'+i))+".txt"),
			[]byte("tài liệu công trình xây dựng nghiệm thu vật tư số "+string(rune('0'+i))), 0644)
	}

	eng, err := New(DefaultConfig(root, filepath.Join(d, ".idx")))
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	if err := eng.Index(d); err != nil {
		t.Fatalf("first index: %v", err)
	}
	first := eng.ChunkCount()
	if first == 0 {
		t.Fatal("first index produced 0 chunks")
	}

	// Index the SAME directory two more times — must stay flat.
	if err := eng.Index(d); err != nil {
		t.Fatalf("second index: %v", err)
	}
	if err := eng.Index(d); err != nil {
		t.Fatalf("third index: %v", err)
	}
	after := eng.ChunkCount()

	if after != first {
		t.Errorf("re-indexing created duplicates: %d chunks after first, %d after three indexes (expected unchanged)", first, after)
	}
	t.Logf("chunk count stable across 3 indexes: %d", after)
}

// A file under a junk dir (build/node_modules/etc.) must never be indexed.
func TestJunkDirSkipped(t *testing.T) {
	root, _ := filepath.Abs("../..")
	if _, err := os.Stat(filepath.Join(root, "models/onnx/bge-m3/model.onnx")); err != nil {
		t.Skip("no model")
	}

	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "real.txt"), []byte("tài liệu thật công trình"), 0644)
	junk := filepath.Join(d, "node_modules", "pkg")
	os.MkdirAll(junk, 0755)
	os.WriteFile(filepath.Join(junk, "junk.txt"), []byte("rác thư viện không nên index"), 0644)
	build := filepath.Join(d, "build", "checkouts")
	os.MkdirAll(build, 0755)
	os.WriteFile(filepath.Join(build, "lorem.txt"), []byte("lorem ipsum build artifact rác"), 0644)

	eng, err := New(DefaultConfig(root, filepath.Join(d, ".idx")))
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	if err := eng.Index(d); err != nil {
		t.Fatalf("index: %v", err)
	}

	// Only real.txt should be indexed; junk dirs skipped.
	res, _ := eng.Search("rác thư viện", 5)
	for _, r := range res {
		if filepath.Base(r.FilePath) == "junk.txt" || filepath.Base(r.FilePath) == "lorem.txt" {
			t.Errorf("junk-dir file was indexed: %s", r.FilePath)
		}
	}
	t.Logf("junk dirs skipped, only real files indexed (%d chunks)", eng.ChunkCount())
}

// Indexing a TARGET that is itself inside a junk tree must produce nothing
// (legacy dirs.json pointed straight at build/SourcePackages/checkouts/...).
func TestIndexTargetInsideJunkSkipped(t *testing.T) {
	root, _ := filepath.Abs("../..")
	if _, err := os.Stat(filepath.Join(root, "models/onnx/bge-m3/model.onnx")); err != nil {
		t.Skip("no model")
	}

	d := t.TempDir()
	target := filepath.Join(d, "build", "SourcePackages", "checkouts", "pkg")
	os.MkdirAll(target, 0755)
	os.WriteFile(filepath.Join(target, "lorem.txt"), []byte("lorem ipsum sample text rác"), 0644)

	eng, err := New(DefaultConfig(root, filepath.Join(d, ".idx")))
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	// Index the junk target directly — must add 0 chunks.
	if err := eng.Index(target); err != nil {
		t.Fatalf("index: %v", err)
	}
	if c := eng.ChunkCount(); c != 0 {
		t.Errorf("indexing a junk-tree target added %d chunks (expected 0)", c)
	}
}

func TestPathHasJunkSegment(t *testing.T) {
	cases := map[string]bool{
		"/Users/x/Documents/rmit/report.pdf":                                false,
		"/Users/x/Desktop/app/build/SourcePackages/checkouts/lib/a.txt":     true,
		"/Users/x/proj/venv/lib/python3.12/site-packages/scipy/data/b.txt":  true,
		"/Users/x/proj/node_modules/pkg/readme.txt":                         true,
		"/Users/x/Documents/work/site-packages-notes.txt":                   false, // not a dir segment
		"/Users/x/proj/.venv/x.txt":                                         true,
	}
	for p, want := range cases {
		if got := pathHasJunkSegment(p); got != want {
			t.Errorf("pathHasJunkSegment(%q) = %v, want %v", p, got, want)
		}
	}
}

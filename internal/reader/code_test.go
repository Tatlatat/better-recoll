package reader

import (
	"os"
	"path/filepath"
	"testing"
)

// Source code and config files are plain UTF-8 text. The tool's whole point is
// "find files by content meaning" — for a developer, code IS the content. These
// extensions must be readable by the same TxtReader that handles .txt/.md.
func TestCodeExtensionsRegistered(t *testing.T) {
	mustHave := []string{
		".go", ".py", ".js", ".ts", ".jsx", ".tsx", ".rs", ".java", ".c", ".h",
		".cpp", ".cc", ".hpp", ".cs", ".rb", ".php", ".swift", ".kt", ".scala",
		".sh", ".bash", ".zsh", ".sql", ".html", ".htm", ".css", ".scss",
		".json", ".yaml", ".yml", ".toml", ".xml", ".ini", ".cfg", ".conf",
		".csv", ".tsv", ".lua", ".pl", ".r", ".m", ".dart", ".vue", ".svelte",
	}
	for _, ext := range mustHave {
		if _, ok := Registry[ext]; !ok {
			t.Errorf("expected code/config extension %s to be registered for indexing", ext)
		}
	}
}

// A real .go file must read back its full text via ReadFile (not error out).
func TestReadFileReadsGoSource(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sample.go")
	src := "package main\n\nfunc main() { println(\"semantic search works\") }\n"
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile(.go) errored: %v", err)
	}
	if got != src {
		t.Fatalf("ReadFile(.go) = %q, want %q", got, src)
	}
}

// Guard the earlier stale test's intent correctly: .txt IS supported now.
func TestTxtIsSupported(t *testing.T) {
	if _, ok := Registry[".txt"]; !ok {
		t.Error(".txt must be registered")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(p)
	if err != nil || got != "hello" {
		t.Fatalf("ReadFile(.txt) = %q, err=%v; want \"hello\", nil", got, err)
	}
}

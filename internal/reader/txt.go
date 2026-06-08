package reader

import (
	"fmt"
	"os"
)

// TxtReader handles plain UTF-8 text: documents (.txt/.md) AND source code /
// config files. Source code is just text — and for a developer "tìm file theo
// nội dung" means searching code, so we read it with the same reader.
type TxtReader struct{}

// textExtensions is the curated set of plain-text file types we index. Kept
// explicit (not "anything") so we don't pull in binary blobs or generated junk
// by extension. All of these are UTF-8 text a person reads.
var textExtensions = []string{
	// documents
	".txt", ".md", ".rst", ".org", ".tex", ".log",
	// go / systems
	".go", ".rs", ".c", ".h", ".cpp", ".cc", ".cxx", ".hpp", ".hh",
	// jvm / .net
	".java", ".kt", ".kts", ".scala", ".groovy", ".cs", ".fs",
	// scripting
	".py", ".rb", ".pl", ".lua", ".r", ".m", ".php", ".tcl",
	// web / frontend
	".js", ".mjs", ".cjs", ".ts", ".jsx", ".tsx", ".vue", ".svelte",
	".html", ".htm", ".css", ".scss", ".sass", ".less",
	// mobile / other langs
	".swift", ".dart", ".ex", ".exs", ".erl", ".clj", ".hs", ".ml",
	// shell / build
	".sh", ".bash", ".zsh", ".fish", ".ps1", ".bat", ".make", ".mk",
	// data / config
	".json", ".jsonc", ".yaml", ".yml", ".toml", ".xml", ".ini",
	".cfg", ".conf", ".properties", ".env", ".csv", ".tsv", ".sql",
	".proto", ".graphql", ".gql",
}

// Extensions returns the file extensions this reader handles.
func (r *TxtReader) Extensions() []string {
	return textExtensions
}

// Read extracts UTF-8 plain text from a text/code/config file.
func (r *TxtReader) Read(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read text file: %w", err)
	}
	return string(data), nil
}

func init() {
	tr := &TxtReader{}
	for _, ext := range textExtensions {
		Registry[ext] = tr
	}
}

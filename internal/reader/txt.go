package reader

import (
	"fmt"
	"os"
)

// TxtReader handles .txt and .md files.
type TxtReader struct{}

// Extensions returns the file extensions this reader handles.
func (r *TxtReader) Extensions() []string {
	return []string{".txt", ".md"}
}

// Read extracts UTF-8 plain text from a .txt or .md file.
func (r *TxtReader) Read(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read text file: %w", err)
	}
	return string(data), nil
}

func init() {
	tr := &TxtReader{}
	Registry[".txt"] = tr
	Registry[".md"] = tr
}

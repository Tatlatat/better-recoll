package reader

import (
	"fmt"
	"os"

	"github.com/fumiama/go-docx"
)

// DocxReader handles .docx files.
type DocxReader struct{}

// Extensions returns the file extensions this reader handles.
func (r *DocxReader) Extensions() []string {
	return []string{".docx"}
}

type stringer interface {
	String() string
}

// Read extracts UTF-8 plain text from a .docx file.
func (r *DocxReader) Read(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open docx file: %w", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to get docx file info: %w", err)
	}

	doc, err := docx.Parse(file, fileInfo.Size())
	if err != nil {
		return "", fmt.Errorf("failed to parse docx file: %w", err)
	}

	var result string
	for _, it := range doc.Document.Body.Items {
		if s, ok := it.(stringer); ok {
			result += s.String() + "\n"
		}
	}

	return result, nil
}

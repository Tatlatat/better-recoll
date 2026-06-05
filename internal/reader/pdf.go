package reader

import (
	"bytes"
	"fmt"

	"github.com/ledongthuc/pdf"
)

// PDFReader handles .pdf files
type PDFReader struct{}

// Extensions returns the file extensions this reader handles.
func (r *PDFReader) Extensions() []string {
	return []string{".pdf"}
}

// Read extracts UTF-8 plain text from a .pdf file.
func (r *PDFReader) Read(path string) (string, error) {
	f, reader, err := pdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open pdf file: %w", err)
	}
	defer f.Close()

	plainTextReader, err := reader.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("failed to get pdf plain text: %w", err)
	}

	var buf bytes.Buffer
	_, err = buf.ReadFrom(plainTextReader)
	if err != nil {
		return "", fmt.Errorf("failed to read pdf text buffer: %w", err)
	}

	return buf.String(), nil
}

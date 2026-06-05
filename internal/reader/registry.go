package reader

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Registry maps file extensions (in lowercase, e.g. ".pdf") to their respective FileReader.
var Registry = map[string]FileReader{
	".pdf":  &PDFReader{},
	".docx": &DocxReader{},
	".xlsx": &XLSXReader{},
}

// ReadFile picks the reader by file extension and extracts plain text from the file.
func ReadFile(path string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	r, ok := Registry[ext]
	if !ok {
		return "", fmt.Errorf("no reader registered for file extension: %s", ext)
	}
	return r.Read(path)
}

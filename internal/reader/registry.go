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
// Bọc recover() vì một số thư viện đọc file (vd ledongthuc/pdf với PDF tiếng Việt
// lỗi) có thể PANIC thay vì trả error — một file xấu không được giết cả chương trình.
func ReadFile(path string) (text string, err error) {
	ext := strings.ToLower(filepath.Ext(path))
	r, ok := Registry[ext]
	if !ok {
		return "", fmt.Errorf("no reader registered for file extension: %s", ext)
	}
	defer func() {
		if rec := recover(); rec != nil {
			text = ""
			err = fmt.Errorf("reader panic on %s: %v", path, rec)
		}
	}()
	return r.Read(path)
}

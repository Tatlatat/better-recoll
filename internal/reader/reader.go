// Package reader ẩn việc trích text từ file sau interface FileReader.
// Mỗi loại file = một reader đăng ký theo đuôi. Pha 2 thêm OCR ảnh/scan
// chỉ là đăng ký thêm một reader, không sửa engine.
package reader

// FileReader trích plain-text UTF-8 (giữ dấu tiếng Việt) từ một file.
type FileReader interface {
	// Extensions trả các đuôi file reader này xử lý, ví dụ {".pdf"}.
	Extensions() []string
	// Read trả về nội dung text của file.
	Read(path string) (text string, err error)
}

// Package store ẩn lưu trữ vector + đoạn sau interface Store.
// Pha 1: file local (mmap/f32). Pha sau: DB nhúng khi quy mô lớn.
package store

// Chunk là một đoạn văn bản đã cắt từ một file, cùng metadata + vector.
type Chunk struct {
	ID            int64
	FilePath      string
	Text          string // bản gốc giữ dấu tiếng Việt
	NormText      string // bản bỏ dấu + lowercase (cho BM25/khớp chữ)
	Offset        int    // vị trí đoạn trong file (để đọc lại đúng đoạn)
	IsBoilerplate bool   // cờ đoạn-khung-chung (template), set bởi DedupeFinder
	Vector        []float32
	ModTime       int64 // Unix giây — thời điểm file sửa lần cuối (recency cho intent)
}

// Store là hợp đồng lưu trữ engine biết.
type Store interface {
	// Write ghi (append) các chunk đã có vector.
	Write(chunks []Chunk) error
	// AllVectors trả toàn bộ vector + id tương ứng, cho flat-scan.
	AllVectors() (vectors [][]float32, ids []int64)
	// GetChunk lấy chunk đầy đủ theo id (để rerank/hiển thị).
	GetChunk(id int64) (Chunk, error)
	// Count số chunk hiện có.
	Count() int
	Close() error
}

// Package model ẩn model embedding/rerank sau interface Embedder.
// Pha 1: ONNX local (onnxruntime-go). Pha sau: HTTP -> TEI/Triton GPU,
// app không đổi một dòng.
package model

// Embedder là hợp đồng duy nhất engine biết về model.
type Embedder interface {
	// Embed trả về dense vector (đã L2-normalize) cho mỗi text.
	Embed(texts []string) ([][]float32, error)
	// Rerank trả điểm liên quan (cross-encoder) của từng text với query.
	Rerank(query string, texts []string) ([]float32, error)
	// Dim là số chiều vector (bge-m3 = 1024).
	Dim() int
}

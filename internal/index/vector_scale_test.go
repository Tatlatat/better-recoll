package index

import (
	"math"
	"math/rand"
	"testing"
	"time"
)

// randNormVec makes a random unit-length 1024-dim vector (same shape as BGE-M3).
func randNormVec(r *rand.Rand, dim int) []float32 {
	v := make([]float32, dim)
	var sum float64
	for i := range v {
		v[i] = float32(r.NormFloat64())
		sum += float64(v[i]) * float64(v[i])
	}
	norm := float32(math.Sqrt(sum))
	if norm == 0 {
		norm = 1
	}
	for i := range v {
		v[i] /= norm
	}
	return v
}

// TestVectorScanScaling đo flat-scan O(N) ở các quy mô để biết CHÍNH XÁC khi nào
// cần ANN. Chạy: go test ./internal/index/ -run TestVectorScanScaling -v
// (Mỗi chunk ~512 ký tự; quy file ≈ chunks/30 để ước số file thực.)
func TestVectorScanScaling(t *testing.T) {
	DisableHNSWForTest = true
	defer func() { DisableHNSWForTest = false }()

	const dim = 1024
	const queries = 30 // đo trung bình trên nhiều query
	r := rand.New(rand.NewSource(42))

	sizes := []int{1000, 5000, 20000, 50000, 100000, 300000, 1000000}

	t.Logf("%-12s %-14s %-12s %s", "vectors", "~files(÷30)", "avg/query", "đánh giá")
	for _, n := range sizes {
		vi := NewVectorIndex(dim)
		for i := 0; i < n; i++ {
			vi.Add(int64(i), randNormVec(r, dim))
		}
		// warm + measure
		q := randNormVec(r, dim)
		_ = vi.Search(q, 10)

		start := time.Now()
		for i := 0; i < queries; i++ {
			vi.Search(randNormVec(r, dim), 10)
		}
		per := time.Since(start) / queries

		verdict := "OK (<100ms)"
		if per > 1000*time.Millisecond {
			verdict = "RẤT CHẬM (>1s) → cần ANN"
		} else if per > 300*time.Millisecond {
			verdict = "CHẬM (>300ms) → nên ANN"
		} else if per > 100*time.Millisecond {
			verdict = "BẮT ĐẦU CHẬM"
		}
		t.Logf("%-12d %-14d %-12s %s", n, n/30, per.Round(time.Millisecond/10).String(), verdict)
	}
}

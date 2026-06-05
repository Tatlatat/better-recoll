package index

import (
	"math"
	"testing"
)

// Oracle Claude. BM25: query khớp token trả doc đó điểm cao nhất.
func TestBM25(t *testing.T) {
	b := NewBM25()
	b.Add(1, "bien ban nghiem thu cong trinh")
	b.Add(2, "bao gia vat tu xay dung")
	b.Add(3, "hop dong thi cong nha xuong")
	b.Build()
	results := b.Search("nghiem thu", 3)
	if len(results) == 0 {
		t.Fatal("BM25 không trả kết quả")
	}
	if results[0].ID != 1 {
		t.Errorf("BM25 top = doc %d, want 1 (chứa 'nghiem thu')", results[0].ID)
	}
}

// Oracle Claude. Vector flat-scan: top-K theo cosine với vector đã normalize.
func TestVectorFlatScan(t *testing.T) {
	v := NewVectorIndex(3)
	v.Add(1, norm3([]float32{1, 0, 0}))
	v.Add(2, norm3([]float32{0, 1, 0}))
	v.Add(3, norm3([]float32{0.9, 0.1, 0}))
	q := norm3([]float32{1, 0, 0})
	results := v.Search(q, 2)
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if results[0].ID != 1 {
		t.Errorf("nearest = %d, want 1", results[0].ID)
	}
	if results[1].ID != 3 {
		t.Errorf("second = %d, want 3 (gần [1,0,0] hơn doc 2)", results[1].ID)
	}
}

func norm3(v []float32) []float32 {
	var s float64
	for _, x := range v {
		s += float64(x * x)
	}
	n := float32(math.Sqrt(s))
	out := make([]float32, len(v))
	for i := range v {
		out[i] = v[i] / n
	}
	return out
}

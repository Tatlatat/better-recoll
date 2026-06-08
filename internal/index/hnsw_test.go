package index

import (
	"math"
	"math/rand"
	"sort"
	"testing"
	"time"
)

// TestHeap validates that the custom minHeap and maxHeap implement standard binary heap operations.
func TestHeap(t *testing.T) {
	// 1. Test minHeap (pops closest first, i.e., smallest dist)
	minH := &minHeap{}
	minH.Push(distPair{idx: 1, dist: 0.5})
	minH.Push(distPair{idx: 2, dist: 0.1})
	minH.Push(distPair{idx: 3, dist: 0.9})
	minH.Push(distPair{idx: 4, dist: 0.3})

	if minH.Len() != 4 {
		t.Errorf("minHeap Len = %d, want 4", minH.Len())
	}

	expectedMin := []float32{0.1, 0.3, 0.5, 0.9}
	for _, expected := range expectedMin {
		p := minH.Pop()
		if math.Abs(float64(p.dist-expected)) > 1e-6 {
			t.Errorf("minHeap Pop got dist %f, want %f", p.dist, expected)
		}
	}

	// 2. Test maxHeap (pops/peeks farthest first, i.e., largest dist)
	maxH := &maxHeap{}
	maxH.Push(distPair{idx: 1, dist: 0.5})
	maxH.Push(distPair{idx: 2, dist: 0.1})
	maxH.Push(distPair{idx: 3, dist: 0.9})
	maxH.Push(distPair{idx: 4, dist: 0.3})

	if maxH.Len() != 4 {
		t.Errorf("maxHeap Len = %d, want 4", maxH.Len())
	}

	if maxH.Peek().dist != 0.9 {
		t.Errorf("maxHeap Peek got %f, want 0.9", maxH.Peek().dist)
	}

	expectedMax := []float32{0.9, 0.5, 0.3, 0.1}
	for _, expected := range expectedMax {
		p := maxH.Pop()
		if math.Abs(float64(p.dist-expected)) > 1e-6 {
			t.Errorf("maxHeap Pop got dist %f, want %f", p.dist, expected)
		}
	}
}

// unitVec makes a random unit-length vector.
func unitVec(r *rand.Rand, dim int) []float32 {
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

// exactTopK performs brute-force exact cosine similarity search.
func exactTopK(vecs [][]float32, ids []int64, q []float32, k int) map[int64]bool {
	type sc struct {
		id int64
		s  float32
	}
	all := make([]sc, len(vecs))
	for i := range vecs {
		var s float32
		for j := range q {
			s += q[j] * vecs[i][j]
		}
		all[i] = sc{ids[i], s}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].s > all[j].s })
	out := make(map[int64]bool, k)
	for i := 0; i < k && i < len(all); i++ {
		out[all[i].id] = true
	}
	return out
}

// clusteredVec generates a vector inside a cluster center with some noise.
func clusteredVec(r *rand.Rand, centers [][]float32, dim int, noise float32) []float32 {
	c := centers[r.Intn(len(centers))]
	v := make([]float32, dim)
	var s float64
	for i := range v {
		v[i] = c[i] + float32(r.NormFloat64())*noise
		s += float64(v[i]) * float64(v[i])
	}
	n := float32(math.Sqrt(s))
	if n == 0 {
		n = 1
	}
	for i := range v {
		v[i] /= n
	}
	return v
}

// Test A — exact dưới ngưỡng (recall phải = 1.0)
func TestHNSWExactBelowThreshold(t *testing.T) {
	const dim = 64
	r := rand.New(rand.NewSource(404))
	vi := NewVectorIndex(dim)
	raw := make([][]float32, 1000)
	for i := 0; i < 1000; i++ {
		v := unitVec(r, dim)
		raw[i] = v
		vi.Add(int64(i), v)
	}

	// Index below threshold must have nil graph (v.nodes == nil)
	if vi.nodes != nil {
		t.Fatalf("graph must be inactive/nil below threshold, got non-nil")
	}

	// Query = bản sao chính xác vector 42 → phải trả 42 đầu tiên, score ~1.0
	got := vi.Search(raw[42], 1)
	if len(got) == 0 || got[0].ID != 42 {
		t.Fatalf("dưới ngưỡng phải exact: got %+v, cần ID 42 đầu tiên", got)
	}
	if got[0].Score < 0.99 {
		t.Fatalf("score trùng hệt phải ~1.0, được %.3f", got[0].Score)
	}
}

// Test B — HNSW recall trên dữ liệu TỔNG HỢP có cụm (≥ ngưỡng)
func TestHNSWRecall10(t *testing.T) {
	if testing.Short() {
		t.Skip("bỏ qua build graph 60k ở -short")
	}
	const dim, n, k, queries = 128, 60000, 10, 50
	r := rand.New(rand.NewSource(101))
	centers := make([][]float32, 60)
	for i := range centers {
		centers[i] = unitVec(r, dim)
	}
	vi := NewVectorIndex(dim)
	var raw [][]float32
	var ids []int64
	for i := 0; i < n; i++ {
		v := clusteredVec(r, centers, dim, 0.10)
		raw = append(raw, v)
		ids = append(ids, int64(i))
		vi.Add(int64(i), v)
	}

	if vi.nodes == nil {
		t.Fatalf("graph must be active/non-nil above threshold")
	}

	var rec float64
	for qi := 0; qi < queries; qi++ {
		q := clusteredVec(r, centers, dim, 0.10)
		truth := exactTopK(raw, ids, q, k)
		got := vi.Search(q, k)
		hit := 0
		for _, g := range got {
			if truth[g.ID] {
				hit++
			}
		}
		rec += float64(hit) / float64(k)
	}
	rec /= float64(queries)
	t.Logf("HNSW recall@10 = %.3f (cần >= 0.95)", rec)
	if rec < 0.95 {
		t.Fatalf("recall@10 = %.3f < 0.95 — HNSW chưa đạt", rec)
	}
}

// Test C — HNSW recall@1 self (chốt chặn quan trọng nhất)
func TestHNSWRecall1Self(t *testing.T) {
	if testing.Short() {
		t.Skip("bỏ qua build graph 60k ở -short")
	}
	const dim, n, queries = 128, 60000, 200
	r := rand.New(rand.NewSource(202))
	vi := NewVectorIndex(dim)
	raw := make([][]float32, n)
	for i := 0; i < n; i++ {
		v := unitVec(r, dim)
		raw[i] = v
		vi.Add(int64(i), v)
	}

	hit := 0
	for qi := 0; qi < queries; qi++ {
		x := r.Intn(n)
		q := make([]float32, dim)
		var s float64
		for i := range q {
			q[i] = raw[x][i] + float32(r.NormFloat64())*0.001
			s += float64(q[i]) * float64(q[i])
		}
		nn := float32(math.Sqrt(s))
		for i := range q {
			q[i] /= nn
		}
		got := vi.Search(q, 1)
		if len(got) > 0 && got[0].ID == int64(x) {
			hit++
		}
	}
	rec := float64(hit) / float64(queries)
	t.Logf("HNSW recall@1(self) = %.3f (cần >= 0.98)", rec)
	if rec < 0.98 {
		t.Fatalf("recall@1 = %.3f < 0.98 — query trùng vector mà không tìm thấy chính nó", rec)
	}
}

// Test D — tốc độ
func TestHNSWSpeed(t *testing.T) {
	if testing.Short() {
		t.Skip("bỏ qua test tốc độ ở -short")
	}
	const dim, n, queries = 128, 200000, 30
	r := rand.New(rand.NewSource(303))
	vi := NewVectorIndex(dim)
	for i := 0; i < n; i++ {
		vi.Add(int64(i), unitVec(r, dim))
	}
	_ = vi.Search(unitVec(r, dim), 10) // warm
	start := time.Now()
	for i := 0; i < queries; i++ {
		vi.Search(unitVec(r, dim), 10)
	}
	per := time.Since(start) / queries
	t.Logf("HNSW tốc độ @200k = %v/query (cần < 50ms)", per.Round(time.Millisecond/10))
	if per > 50*time.Millisecond {
		t.Fatalf("query %v > 50ms ở 200k vector — HNSW chưa đủ nhanh", per)
	}
}

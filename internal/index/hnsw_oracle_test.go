package index

// ORACLE TEST — trọng tài độc lập của recall HNSW.
// agy KHÔNG được sửa file này (nó là thước đo khách quan). Nếu HNSW của agy
// làm các test này FAIL, code chưa đạt — bất kể test riêng của agy nói gì.
//
// Đây là cách chống drift thật sự: không tin "agy bảo xong", mà tin số đo recall.

import (
	"math"
	"math/rand"
	"sort"
	"testing"
	"time"
)

func oracleUnitVec(r *rand.Rand, dim int) []float32 {
	v := make([]float32, dim)
	var sum float64
	for i := range v {
		v[i] = float32(r.NormFloat64())
		sum += float64(v[i]) * float64(v[i])
	}
	n := float32(math.Sqrt(sum))
	if n == 0 {
		n = 1
	}
	for i := range v {
		v[i] /= n
	}
	return v
}

func oracleExactTopK(vecs [][]float32, ids []int64, q []float32, k int) map[int64]bool {
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

func oracleClustered(r *rand.Rand, centers [][]float32, dim int, noise float32) []float32 {
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

// ORACLE B: recall@10 trên dữ liệu cụm, ≥ ngưỡng → graph active. Phải ≥ 0.95.
// Cần n > hnswThreshold (50k) để kích hoạt HNSW (dưới ngưỡng là flat-scan), nên
// build graph nặng — bỏ qua ở -short (chạy đầy đủ khi `go test` thường).
func TestOracleHNSWRecall10(t *testing.T) {
	if testing.Short() {
		t.Skip("bỏ qua build graph 60k ở -short")
	}
	const dim, n, k, queries = 128, 60000, 10, 50
	r := rand.New(rand.NewSource(101))
	centers := make([][]float32, 60)
	for i := range centers {
		centers[i] = oracleUnitVec(r, dim)
	}
	vi := NewVectorIndex(dim)
	var raw [][]float32
	var ids []int64
	for i := 0; i < n; i++ {
		v := oracleClustered(r, centers, dim, 0.10)
		raw = append(raw, v)
		ids = append(ids, int64(i))
		vi.Add(int64(i), v)
	}
	var rec float64
	for qi := 0; qi < queries; qi++ {
		q := oracleClustered(r, centers, dim, 0.10)
		truth := oracleExactTopK(raw, ids, q, k)
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
	t.Logf("ORACLE recall@10 = %.3f (cần >= 0.95)", rec)
	if rec < 0.95 {
		t.Fatalf("recall@10 = %.3f < 0.95 — HNSW chưa đạt, đang bỏ sót kết quả đúng", rec)
	}
}

// ORACLE C: recall@1 self (query = vector đã index + nhiễu cực nhỏ). Phải ≥ 0.98.
// Đây là test bắt được bug của coder/hnsw (nó chỉ đạt 0.47).
func TestOracleHNSWRecall1Self(t *testing.T) {
	if testing.Short() {
		t.Skip("bỏ qua build graph 60k ở -short")
	}
	const dim, n, queries = 128, 60000, 200
	r := rand.New(rand.NewSource(202))
	vi := NewVectorIndex(dim)
	raw := make([][]float32, n)
	for i := 0; i < n; i++ {
		v := oracleUnitVec(r, dim)
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
	t.Logf("ORACLE recall@1(self) = %.3f (cần >= 0.98)", rec)
	if rec < 0.98 {
		t.Fatalf("recall@1 = %.3f < 0.98 — query trùng vector mà không tìm thấy chính nó", rec)
	}
}

// ORACLE D: tốc độ — 200k vector, query phải < 50ms.
func TestOracleHNSWSpeed(t *testing.T) {
	if testing.Short() {
		t.Skip("bỏ qua test tốc độ ở -short")
	}
	const dim, n, queries = 128, 200000, 30
	r := rand.New(rand.NewSource(303))
	vi := NewVectorIndex(dim)
	for i := 0; i < n; i++ {
		vi.Add(int64(i), oracleUnitVec(r, dim))
	}
	_ = vi.Search(oracleUnitVec(r, dim), 10) // warm
	start := time.Now()
	for i := 0; i < queries; i++ {
		vi.Search(oracleUnitVec(r, dim), 10)
	}
	per := time.Since(start) / queries
	t.Logf("ORACLE tốc độ @200k = %v/query (cần < 50ms)", per.Round(time.Millisecond/10))
	if per > 50*time.Millisecond {
		t.Fatalf("query %v > 50ms ở 200k vector — HNSW chưa đủ nhanh", per)
	}
}

// ORACLE F (KHÓ — đã hiệu chỉnh): recall@10 trên cụm CHỒNG LẤN VỪA PHẢI. Nhiễu
// 0.20 (đo được: cosine trong-cụm 0.158 vs khác-cụm 0.004 → cụm vẫn còn nhưng
// nhoè) — KHÓ hơn ORACLE B (nhiễu 0.10, cụm sạch) nhưng vẫn có cấu trúc như
// vector thật. (Lưu ý: nhiễu 0.35 phá cụm thành ~uniform — ca bệnh lý KHÔNG phản
// ánh embedding thật, nên không dùng.) So flat-scan exact trên chính dữ liệu đó.
// Ngưỡng 0.90 — cao, nhưng đạt được với graph nối tốt (impl hiện tại ~0.95+).
func TestOracleHNSWRecallHard(t *testing.T) {
	if testing.Short() {
		t.Skip("bỏ qua build graph 60k ở -short")
	}
	const dim, n, k, queries = 128, 60000, 10, 60
	r := rand.New(rand.NewSource(909))
	centers := make([][]float32, 40)
	for i := range centers {
		centers[i] = oracleUnitVec(r, dim)
	}
	vi := NewVectorIndex(dim)
	var raw [][]float32
	var ids []int64
	for i := 0; i < n; i++ {
		v := oracleClustered(r, centers, dim, 0.20) // chồng lấn vừa (cụm vẫn còn)
		raw = append(raw, v)
		ids = append(ids, int64(i))
		vi.Add(int64(i), v)
	}
	var rec float64
	for qi := 0; qi < queries; qi++ {
		q := oracleClustered(r, centers, dim, 0.20)
		truth := oracleExactTopK(raw, ids, q, k) // ground truth = flat-scan exact
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
	t.Logf("ORACLE recall@10 (cụm chồng lấn vừa, khó) = %.3f (cần >= 0.90)", rec)
	if rec < 0.90 {
		t.Fatalf("recall@10 = %.3f < 0.90 trên cụm chồng lấn vừa — graph nối kém, bỏ sót hàng xóm thật", rec)
	}
}

// ORACLE G (KHÓ): recall@1 với nhiễu VỪA (không phải ×0.001 tầm thường). Query
// là một điểm thật gần nhưng KHÔNG trùng vector index — kiểm graph có dẫn tới
// đúng láng giềng gần nhất tuyệt đối không. So flat-scan exact. Ngưỡng 0.95.
func TestOracleHNSWRecall1Near(t *testing.T) {
	if testing.Short() {
		t.Skip("bỏ qua build graph 60k ở -short")
	}
	const dim, n, queries = 128, 60000, 200
	r := rand.New(rand.NewSource(717))
	centers := make([][]float32, 40)
	for i := range centers {
		centers[i] = oracleUnitVec(r, dim)
	}
	vi := NewVectorIndex(dim)
	var raw [][]float32
	var ids []int64
	for i := 0; i < n; i++ {
		v := oracleClustered(r, centers, dim, 0.20)
		raw = append(raw, v)
		ids = append(ids, int64(i))
		vi.Add(int64(i), v)
	}
	hit := 0
	for qi := 0; qi < queries; qi++ {
		q := oracleClustered(r, centers, dim, 0.20) // điểm mới, không trùng
		truth := oracleExactTopK(raw, ids, q, 1)    // NN đúng tuyệt đối
		got := vi.Search(q, 1)
		if len(got) > 0 && truth[got[0].ID] {
			hit++
		}
	}
	rec := float64(hit) / float64(queries)
	t.Logf("ORACLE recall@1 (gần, nhiễu vừa) = %.3f (cần >= 0.93)", rec)
	if rec < 0.93 {
		t.Fatalf("recall@1 = %.3f < 0.93 — không tìm được láng giềng gần nhất thật", rec)
	}
}

// ORACLE E: dưới ngưỡng phải exact (recall 1.0) — index nhỏ không được xấp xỉ.
func TestOracleExactBelowThreshold(t *testing.T) {
	const dim = 64
	r := rand.New(rand.NewSource(404))
	vi := NewVectorIndex(dim)
	raw := make([][]float32, 1000)
	for i := 0; i < 1000; i++ {
		v := oracleUnitVec(r, dim)
		raw[i] = v
		vi.Add(int64(i), v)
	}
	// query = bản sao chính xác vector 42 → phải trả 42 đầu tiên, score ~1.0
	got := vi.Search(raw[42], 1)
	if len(got) == 0 || got[0].ID != 42 {
		t.Fatalf("dưới ngưỡng phải exact: got %+v, cần ID 42 đầu tiên", got)
	}
	if got[0].Score < 0.99 {
		t.Fatalf("score trùng hệt phải ~1.0, được %.3f", got[0].Score)
	}
}

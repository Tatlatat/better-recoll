# SPEC: Viết HNSW thuần Go đúng chuẩn cho VectorIndex

> ĐỌC KỸ TOÀN BỘ TRƯỚC KHI VIẾT. Đây là hợp đồng. Bạn (agy) là người viết code.
> KHÔNG được đổi mục tiêu, KHÔNG thêm tính năng ngoài spec, KHÔNG đổi public API.
> Mỗi bước có lệnh kiểm thử BẮT BUỘC chạy và phải PASS trước khi sang bước sau.
> Nếu một test FAIL: sửa code cho tới khi PASS. KHÔNG sửa test để né. KHÔNG bỏ qua.

---

## 0. MỤC TIÊU (đọc lại mỗi khi bắt đầu một bước)

Thay thuật toán tìm vector từ **flat-scan O(N)** sang **HNSW O(log N)** để tìm
nhanh ở quy mô lớn (hàng trăm nghìn → triệu vector), **NHƯNG độ chính xác phải
giữ**: recall@10 ≥ 0.95 so với flat-scan exact trên vector thật.

**Lý do làm lại:** hai thư viện HNSW Go có sẵn (coder/hnsw, TFMV/hnsw) cho
recall@1 ≈ 0.47 trên vector BGE-M3 thật — HỎNG (bỏ sót >50% file đúng). Bug gốc:
quản lý heap candidate sai (vứt nhầm phần tử khi đầy) + điều kiện dừng greedy sai.
File này phải làm ĐÚNG thuật toán theo paper Malkov & Yashunin 2016.

**Tiêu chí "xong" (Definition of Done) — TẤT CẢ phải đạt:**
- [ ] `go build ./...` xanh, KHÔNG thêm dependency ngoài (thuần Go, không cgo).
- [ ] `go vet ./internal/index/` sạch.
- [ ] Test recall (Bước 5) PASS: recall@10 ≥ 0.95 trên vector thật + tổng hợp.
- [ ] Test tốc độ (Bước 5) PASS: ở 200k vector, query < 50ms (nhanh hơn flat-scan ≥5x).
- [ ] Test exact-below-threshold (Bước 5) PASS: index nhỏ vẫn recall = 1.0.
- [ ] `go test ./internal/...` TOÀN BỘ xanh (không phá test cũ).
- [ ] Public API của VectorIndex KHÔNG đổi (xem mục 2).

---

## 1. RÀNG BUỘC TUYỆT ĐỐI (vi phạm = sai)

1. **KHÔNG thêm import ngoài.** Chỉ dùng stdlib Go (`sort`, `math`, `math/rand`,
   `sync`, `container/heap` nếu cần). KHÔNG `go get` gì cả. Lý do: sản phẩm là
   1 binary thuần Go chạy được trên Windows công ty xây dựng, không cgo.
2. **KHÔNG đổi public API** của `VectorIndex` (mục 2). Engine gọi nó ở 8 chỗ;
   đổi chữ ký = vỡ build.
3. **Vector đã được L2-normalize sẵn** (chuẩn hoá). Nên cosine similarity =
   dot product. `Result.Score` = cosine similarity, **CAO = TỐT** (giống flat-scan cũ).
4. **Key (ID) là `int64`.** Là ChunkID. Phải trả đúng ID này về.
5. **Determinism:** với cùng seed RNG, kết quả phải lặp lại được (cho test).
6. KHÔNG sửa file nào ngoài `internal/index/vector.go` và các file test trong
   `internal/index/`. KHÔNG đụng engine, webui, store.

---

## 2. PUBLIC API BẮT BUỘC GIỮ NGUYÊN (chữ ký không đổi một ký tự)

File `internal/index/vector.go`, package `index`. Phải có ĐÚNG các hàm này với
ĐÚNG chữ ký (engine phụ thuộc):

```go
package index

// Result đã có sẵn trong bm25.go — KHÔNG định nghĩa lại:
//   type Result struct { ID int64; Score float32 }

type VectorIndex struct { /* nội bộ bạn tự thiết kế */ }

func NewVectorIndex(dim int) *VectorIndex            // tạo index rỗng, dim chiều
func (v *VectorIndex) Add(id int64, vec []float32)   // thêm 1 vector
func (v *VectorIndex) Search(query []float32, k int) []Result // top-k, Score=cosine desc
```

Hành vi bắt buộc của `Search`:
- Trả ≤ k kết quả, **sắp xếp Score giảm dần** (cosine cao nhất trước).
- Index rỗng hoặc k≤0 → trả `nil`.
- Score là cosine similarity (1.0 = trùng hệt, ~0 = không liên quan).

---

## 3. THIẾT KẾ (kiến trúc HYBRID — làm đúng cái này)

`VectorIndex` giữ **cả hai**:
1. **Flat store** (`ids []int64`, `vectors [][]float32`) — LUÔN giữ. Dùng cho:
   (a) exact search khi index nhỏ, (b) nguồn sự thật để build/rebuild graph,
   (c) ground-truth cho test recall.
2. **HNSW graph** — chỉ kích hoạt khi `len(ids) >= hnswThreshold`.

```
const hnswThreshold = 50000  // dưới ngưỡng: flat exact (recall 1.0, <100ms đã đo)
```

`Add`:
- Luôn append vào flat store.
- Nếu graph đã active → `graphInsert(id, vec)`.
- Nếu chưa active VÀ vừa chạm ngưỡng → `buildGraph()` (build 1 lần từ toàn bộ flat).

`Search`:
- graph == nil → `searchFlat` (copy nguyên hàm flat-scan cũ, exact).
- graph != nil → `searchHNSW`.

→ Dưới 50k vector: exact, recall = 1.0. Trên 50k: HNSW, recall ≥ 0.95.
Đây là điểm mấu chốt: KHÔNG bao giờ hy sinh recall ở quy mô nhỏ (đa số người dùng).

---

## 4. THUẬT TOÁN HNSW ĐÚNG (theo Malkov & Yashunin 2016) — VIẾT CHÍNH XÁC

### 4.1 Tham số
```
M        = 16    // số neighbor tối đa mỗi node (layer > 0)
Mmax0    = 32    // số neighbor tối đa ở layer 0 (= 2*M)
efConstruction = 200  // độ rộng beam khi XÂY (cao = graph tốt hơn, xây chậm hơn)
efSearch = 100   // độ rộng beam khi TÌM (cao = recall cao hơn, tìm chậm hơn). PHẢI >= k.
mL       = 1 / ln(M)  // hệ số chuẩn hoá tầng (level normalization)
```

### 4.2 Cấu trúc
- Mỗi node: `id int64`, `vec []float32`, `level int`, và **neighbors theo từng tầng**:
  `neighbors [][]int64` (neighbors[l] = danh sách ID hàng xóm ở tầng l).
- Graph: danh sách node (map id→node hoặc slice), `entryPoint int64`, `maxLevel int`.
- Khoảng cách: dùng **distance = 1 - cosine = 1 - dotProduct** (vì vector đã normalize).
  GẦN = distance NHỎ. (Lưu ý: nội bộ HNSW làm việc với "khoảng cách", nhỏ = gần.
  Khi trả kết quả ra ngoài thì đổi lại: Score = 1 - distance = cosine.)

### 4.3 Hàm chọn level cho node mới (randomLevel)
```
level = floor(-ln(rand_0_1()) * mL)
```

### 4.4 INSERT (thuật toán 1 trong paper) — viết đúng:
```
1. l = randomLevel()
2. ep = entryPoint; nếu graph rỗng: node thành entryPoint, maxLevel=l, return.
3. // Đi từ maxLevel xuống l+1: greedy tìm 1 điểm gần nhất ở mỗi tầng (ef=1)
   for lc = maxLevel downto l+1:
       W = searchLayer(q=vec, ep, ef=1, layer=lc)
       ep = điểm gần nhất trong W
4. // Từ min(maxLevel,l) xuống 0: tìm ef ứng viên, chọn M neighbor, nối 2 chiều
   for lc = min(maxLevel, l) downto 0:
       W = searchLayer(q=vec, ep, ef=efConstruction, layer=lc)
       neighbors = SELECT-NEIGHBORS-HEURISTIC(vec, W, M, layer=lc)  // mục 4.6
       thêm liên kết 2 chiều giữa node mới và mỗi neighbor ở tầng lc
       // pruning: với mỗi neighbor, nếu nó có > Mmax (Mmax0 nếu lc==0) hàng xóm,
       //          chạy lại SELECT-NEIGHBORS-HEURISTIC để cắt xuống Mmax.
       ep = W
5. nếu l > maxLevel: entryPoint = node mới; maxLevel = l.
```

### 4.5 SEARCH-LAYER (thuật toán 2) — ĐÂY LÀ CHỖ HAI LIB KIA SAI. VIẾT CỰC CẨN THẬN:
```
searchLayer(q, entryPoints, ef, layer) -> trả ef điểm gần nhất ở tầng `layer`:
  visited = set(entryPoints)
  candidates = min-heap theo distance(q, .)   // LẤY GẦN NHẤT ra trước
  W = max-heap theo distance(q, .)             // GIỮ ef điểm gần nhất; đỉnh = XA nhất
  cho mỗi ep: push vào candidates và W
  while candidates không rỗng:
      c = candidates.popMin()        // điểm gần nhất chưa duyệt
      f = W.peekMax()                // điểm xa nhất hiện trong W
      if distance(c) > distance(f):  // ĐIỀU KIỆN DỪNG ĐÚNG: không còn hy vọng cải thiện
          break
      for mỗi neighbor e của c ở tầng `layer`:
          if e đã visited: continue
          visited.add(e)
          f = W.peekMax()
          if distance(q,e) < distance(f) OR W.size() < ef:
              candidates.push(e)
              W.push(e)
              if W.size() > ef:
                  W.popMax()         // BỎ ĐÚNG ĐIỂM XA NHẤT (KHÔNG phải phần tử cuối mảng!)
  return W (ef điểm)
```
**SAI LẦM CHẾT NGƯỜI cần tránh (chính là bug của coder/hnsw):**
- KHÔNG dùng "PopLast" / bỏ phần tử cuối slice khi heap đầy. PHẢI bỏ phần tử
  XA NHẤT (max distance). Dùng max-heap cho W và popMax() đúng nghĩa.
- Điều kiện dừng PHẢI là `distance(nearest_candidate) > distance(farthest_in_W)`,
  KHÔNG dùng cờ "improved" lỏng lẻo.
- `container/heap` của Go: nếu dùng, nhớ heap là MIN-heap; làm max-heap bằng cách
  đảo dấu so sánh trong Less(). Test kỹ popMax thật sự bỏ phần tử xa nhất.

### 4.6 SELECT-NEIGHBORS-HEURISTIC (thuật toán 4) — quan trọng cho recall:
```
selectNeighborsHeuristic(q, candidates W, M, layer):
  // KHÔNG chỉ lấy M điểm gần nhất (thuật toán 3 đơn giản, recall kém hơn).
  // Dùng heuristic: ưu tiên điểm gần q NHƯNG đa dạng hướng (tránh chùm 1 phía).
  result = []
  candidates_sorted = W sắp tăng theo distance(q, .)
  for e in candidates_sorted:
      if len(result) >= M: break
      // nhận e nếu e gần q HƠN là gần bất kỳ điểm nào đã có trong result
      good = true
      for r in result:
          if distance(e, r) < distance(e, q):
              good = false; break
      if good: result.append(e)
  // (tuỳ chọn) nếu result < M, bù thêm các điểm gần nhất còn lại.
  return result
```

---

## 5. KIỂM THỬ BẮT BUỘC (file `internal/index/hnsw_test.go`)

Viết các test sau. CHẠY và PASS hết trước khi coi là xong. Lệnh ở mục 7.

### Test A — exact dưới ngưỡng (recall phải = 1.0)
- Thêm 1000 vector dim=64 ngẫu nhiên (đã normalize). graph PHẢI nil.
- Query = bản sao của vector thứ 42. Search(q,1)[0].ID PHẢI == 42, Score≈1.0.

### Test B — HNSW recall trên dữ liệu TỔNG HỢP có cụm (≥ ngưỡng)
- Tạo 60 tâm cụm ngẫu nhiên (dim=128, normalize). Sinh 60000 vector = tâm + nhiễu nhỏ.
- graph PHẢI active.
- 50 query (mỗi query = 1 vector cụm mới). So top-10 HNSW vs flat-exact top-10.
- **recall@10 trung bình PHẢI ≥ 0.95.** Nếu < 0.95: thuật toán còn sai, sửa tiếp.

### Test C — HNSW recall@1 self (chốt chặn quan trọng nhất)
- 20000 vector dim=128. Query = vector[i] + nhiễu CỰC NHỎ (×0.001) cho 200 i ngẫu nhiên.
- NN đúng phải là chính i. **recall@1 PHẢI ≥ 0.98.**
  (Đây là test bắt được bug của coder/hnsw: nó chỉ đạt 0.47.)

### Test D — tốc độ
- 200000 vector dim=128. Đo trung bình 30 query. **PHẢI < 50ms/query.**
  (Flat-scan ở mức này ~190ms — HNSW phải nhanh hơn rõ.)

### Test E — không phá flat scan cũ
- Giữ/đảm bảo test cũ `TestVectorFlatScan` và `TestVectorScanScaling` vẫn PASS.

Helper gợi ý (tự viết trong test): `unitVec(r, dim)` sinh vector chuẩn hoá;
`exactTopK(vecs, ids, q, k)` brute-force ground truth bằng dot-product.

---

## 6. QUY TRÌNH LÀM (theo thứ tự, KHÔNG nhảy bước)

- **B1.** Đọc `internal/index/vector.go` hiện tại (flat scan) + `bm25.go` (để thấy
  `Result`). Hiểu API phải giữ.
- **B2.** Viết khung: struct VectorIndex hybrid (flat + graph nil), giữ nguyên
  `searchFlat` từ code cũ. Build xanh. Chạy Test A + E → PASS (chưa cần HNSW).
- **B3.** Viết `searchLayer` (mục 4.5) + 2 heap (min cho candidates, max cho W).
  Viết test đơn vị nhỏ cho heap: popMax phải trả phần tử xa nhất. PASS.
- **B4.** Viết `buildGraph`, `graphInsert` (INSERT 4.4), `selectNeighborsHeuristic`
  (4.6), `searchHNSW`. Nối vào Add/Search.
- **B5.** Chạy Test B, C, D. Nếu recall < ngưỡng: kiểm lại searchLayer (điều kiện
  dừng + popMax), kiểm selectNeighbors. Sửa tới khi PASS.
- **B6.** Chạy TOÀN BỘ `go test ./internal/...` + `go vet`. Tất cả xanh.
- **B7.** Tự rà mục 0 (Definition of Done) — tick hết mới báo xong.

---

## 7. LỆNH KIỂM THỬ (chạy đúng các lệnh này; SFS_ROOT để tìm model nếu cần)

```bash
cd "/Users/tatlatat/Documents/level 3"

# build + vet
go build ./... 2>&1 | grep -v "duplicate librar"
go vet ./internal/index/

# test HNSW (recall + speed). -timeout cao vì build graph 200k mất thời gian.
go test ./internal/index/ -run "TestVectorIndexExact|TestHNSW|TestVectorFlatScan" -v -timeout 600s 2>&1 | grep -v "duplicate librar"

# test toàn bộ (không được phá gì)
go test ./internal/... -timeout 900s 2>&1 | grep -v "duplicate librar" | tail -20
```

**PASS nghĩa là:** mọi dòng `--- PASS`, dòng cuối `ok`, KHÔNG có `FAIL`.
Đặc biệt nhìn log recall: phải thấy `recall@10 >= 0.95` và `recall@1 >= 0.98`.

---

## 8. CHỐNG DRIFT (đọc nếu thấy mình đang lạc)

Nếu bạn thấy mình đang: thêm dependency, đổi API, sửa test cho dễ pass, bỏ qua
recall thấp, "tối ưu" ngoài spec, hay làm file khác — **DỪNG**. Quay lại mục 0.
Mục tiêu DUY NHẤT: VectorIndex tìm nhanh ở quy mô lớn MÀ recall@10 ≥ 0.95.
Recall là vua. Code chạy mà recall thấp = THẤT BẠI, không phải "gần xong".

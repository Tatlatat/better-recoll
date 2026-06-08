# Prompt dán vào agy CLI — viết lại HNSW

> Đây là PROMPT để bạn tự copy-paste vào agy, KHÔNG phải lệnh shell.
> Có 2 bản: (A) bản khởi động ngắn, (B) bản tiếp-tục khi gate báo lỗi.
> Cơ chế chống-drift nằm ở SỐ ĐO RECALL của oracle test, không ở trí nhớ của agy.

---

## ════════ PROMPT A — DÁN ĐẦU TIÊN (giao việc) ════════

```
Bạn là người viết code Go. Dự án "better-recoll" tại /Users/tatlatat/Documents/level 3.

NHIỆM VỤ DUY NHẤT: viết lại internal/index/vector.go từ flat-scan O(N) thành HNSW
thuần Go O(log N) đúng chuẩn Malkov & Yashunin 2016, GIỮ NGUYÊN độ chính xác.

TRƯỚC KHI VIẾT MỘT DÒNG NÀO: đọc HẾT file docs/agy-tasks/HNSW_SPEC.md và làm ĐÚNG
y theo nó. Spec là hợp đồng — không đổi mục tiêu, không thêm tính năng, không đổi
public API. Cũng đọc internal/index/vector.go (flat-scan hiện tại) và internal/
index/bm25.go (định nghĩa Result) để biết API phải giữ.

RÀNG BUỘC CỨNG (vi phạm = sai):
- Thuần Go, KHÔNG cgo, KHÔNG go get / không thêm dependency ngoài (chỉ stdlib:
  sort, math, math/rand, sync, container/heap).
- KHÔNG đổi chữ ký public của VectorIndex: NewVectorIndex(dim int) *VectorIndex,
  (v) Add(id int64, vec []float32), (v) Search(query []float32, k int) []Result.
  Result{ID int64; Score float32}, Score = cosine, CAO = TỐT, sort giảm dần.
- Kiến trúc HYBRID: dưới 50000 vector dùng flat-scan exact (recall 1.0); từ 50000
  trở lên mới bật HNSW graph. KHÔNG hy sinh recall ở quy mô nhỏ.
- CHỈ sửa internal/index/vector.go và file test trong internal/index/. KHÔNG đụng
  engine, webui, store.

CHỖ DỄ SAI NHẤT — searchLayer (mục 4.5 trong spec). Hai thư viện Go có sẵn HỎNG ở
đây (recall sập còn 0.47) vì 2 lỗi, ĐỪNG lặp lại:
1. Khi heap kết quả W đầy (> ef), phải bỏ ĐÚNG phần tử XA NHẤT (popMax của max-heap),
   KHÔNG bỏ phần tử cuối mảng ("PopLast").
2. Điều kiện dừng phải là: distance(ứng_viên_gần_nhất) > distance(xa_nhất_trong_W)
   thì mới break. KHÔNG dùng cờ "improved" lỏng lẻo.
Dùng container/heap (min-heap) làm max-heap bằng cách đảo dấu Less(). Viết 1 unit
test nhỏ chứng minh popMax thật sự trả phần tử xa nhất TRƯỚC khi tin nó.

selectNeighborsHeuristic (mục 4.6): dùng heuristic đa dạng-hướng (thuật toán 4),
KHÔNG chỉ lấy M điểm gần nhất.

KIỂM THỬ: viết internal/index/hnsw_test.go đúng mục 5 của spec (Test A exact dưới
ngưỡng, B recall@10≥0.95 cụm, C recall@1≥0.98 self, D tốc độ <50ms@200k, E không
phá flat cũ). Làm theo quy trình B1..B7 mục 6. Chạy lệnh kiểm thử mục 7 và đảm bảo
MỌI test PASS.

QUAN TRỌNG: có file internal/index/hnsw_oracle_test.go là TRỌNG TÀI ĐỘC LẬP của tôi.
TUYỆT ĐỐI KHÔNG sửa, không xóa, không hạ ngưỡng trong file đó. Code của bạn phải làm
nó PASS (TestOracleHNSWRecall10 ≥0.95, TestOracleHNSWRecall1Self ≥0.98,
TestOracleHNSWSpeed <50ms, TestOracleExactBelowThreshold recall 1.0). Tôi sẽ chạy
oracle này để chấm — agy "bảo xong" không tính, chỉ số đo recall mới tính.

RECALL LÀ VUA. Code chạy mà recall thấp = CHƯA XONG, không phải "gần xong". Bắt đầu
bằng việc đọc spec rồi báo lại tóm tắt thiết kế trước khi code.
```

---

## ════════ PROMPT B — DÁN KHI GATE BÁO LỖI (vòng sửa) ════════

> Sau khi agy nói xong, tôi (Claude) chạy oracle test chấm. Nếu FAIL, dán prompt
> này kèm log lỗi tôi đưa cho bạn. Thay phần `<<LOG>>` bằng log thật.

```
Gate recall CHƯA đạt. Đây là kết quả chạy oracle test (trọng tài độc lập):

<<LOG>>

Sửa internal/index/vector.go (và hnsw_test.go nếu test bạn viết sai cách, NHƯNG
KHÔNG được hạ ngưỡng recall, KHÔNG sửa file hnsw_oracle_test.go). Bám sát spec,
đặc biệt:
- Mục 4.5 searchLayer: popMax phải bỏ điểm XA NHẤT; điều kiện dừng đúng
  (dist(nearest_candidate) > dist(farthest_in_W)).
- Mục 4.6 selectNeighborsHeuristic: heuristic đa dạng hướng.
- Nếu recall@1 self thấp: gần như chắc chắn searchLayer quản lý heap W sai.
- Nếu recall@10 thấp nhưng @1 ổn: tăng hiệu quả selectNeighbors / kiểm efSearch>=k.
Mục tiêu giữ nguyên: recall@10≥0.95, recall@1≥0.98, <50ms@200k. Sửa xong tự chạy
lại lệnh test mục 7 của spec.
```

---

## Cách tôi (Claude) chấm sau mỗi lần agy báo xong

Tôi chạy đúng oracle (agy không được đụng file này):

```bash
cd "/Users/tatlatat/Documents/level 3"
go vet ./internal/index/
go test ./internal/index/ -run "TestOracle" -v -timeout 900s 2>&1 | grep -v "duplicate librar"
go test ./internal/... -timeout 900s 2>&1 | grep -v "duplicate librar" | tail -15
```

PASS = mọi dòng `--- PASS`, thấy `recall@10 >= 0.95` và `recall@1 >= 0.98`, dòng
cuối `ok`, KHÔNG có `FAIL`. Nếu FAIL → đưa log vào PROMPT B, dán lại cho agy.

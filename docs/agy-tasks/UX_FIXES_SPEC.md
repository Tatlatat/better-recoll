# SPEC: Sửa 4 root-cause về trải nghiệm khởi động (load lâu + churn mỗi boot)

> ĐỌC HẾT TRƯỚC KHI SỬA. Đây là hợp đồng. Bạn (agy) là người viết code.
> Mỗi fix là MỘT thay đổi nhỏ, có TEST riêng. Chạy test xanh mới sang fix kế.
> KHÔNG đổi public API, KHÔNG thêm dependency, KHÔNG đụng internal/index/ (vùng khác đang sửa HNSW).

---

## BỐI CẢNH (đã điều tra root-cause bằng đo thật, đừng đoán lại)

Đo được: `engine.New` mất **3.73s** (index rỗng) — toàn bộ là LOAD MODEL, không phải
inference (first-search chỉ 27ms). Nguyên nhân + 3 lỗi churn-khi-boot đã xác định
chính xác qua đọc code. Nhiệm vụ: sửa đúng 4 root-cause dưới.

---

## FIX 1 — Load model 2 LẦN (gốc rễ "load lâu"). ƯU TIÊN CAO NHẤT.

**Bằng chứng:** `internal/engine/engine.go` func `New`:
- dòng ~93: `embedder` = NewOnnxEmbedder (bge-m3 2.3GB)
- dòng ~99: `indexEmbedder` = NewOnnxEmbedder (bge-m3 2.3GB **LẦN NỮA — trùng**)
- dòng ~125: `reranker` = NewOnnxReranker (571MB)
- dòng ~134: `indexReranker` = NewOnnxReranker (571MB **LẦN NỮA — trùng**)

→ Load ~5.7GB thay vì ~2.9GB. ONNX session là READ-ONLY khi inference, **dùng chung
1 instance cho cả search lẫn index là an toàn** (cùng model, cùng trọng số).

**VIỆC LÀM:**
1. Trong `engine.New`: chỉ tạo MỘT `embedder` và MỘT `reranker`.
2. Gán `indexEmbedder` = cùng instance với `embedder`; `indexReranker` = cùng với `reranker`.
   (Nếu struct Engine có field riêng `indexEmbedder`/`indexReranker`, cho chúng trỏ
   tới cùng object. NHỚ: lúc Close() KHÔNG được Close 2 lần cùng 1 session → sẽ panic
   double-free. Giải pháp: Close() chỉ đóng `embedder` và `reranker`, BỎ việc đóng
   indexEmbedder/indexReranker nếu chúng là cùng instance — hoặc đơn giản đặt
   indexEmbedder=embedder rồi trong Close chỉ đóng mỗi cái một lần.)
3. Kiểm tra mọi nơi dùng `e.indexEmbedder` / `e.indexReranker` (rerank.go, engine.go)
   vẫn chạy đúng — chúng giờ trỏ cùng object nhưng hành vi y hệt (read-only).

**RÀNG BUỘC:** `SafeEmbedder` bọc embedder đã có mutex riêng → share an toàn cho
concurrent search+index. Nếu lo race, để `indexEmbedder` dùng chung CÙNG `*SafeEmbedder`
với `embedder` (mutex sẽ tuần tự hoá). Đừng tạo SafeEmbedder thứ hai bọc cùng session.

**TEST (internal/engine/loadshare_test.go):**
- `TestSingleModelLoad`: tạo engine từ DefaultConfig (cần model thật — nếu thiếu model
  thì `t.Skip`). Assert: `eng.embedder == eng.indexEmbedder` (cùng con trỏ) HOẶC ít
  nhất engine.New chạy < 2.5s (đo time.Since). Search + một lần index nhỏ vẫn trả kết
  quả đúng, không panic, không data race (`go test -race`).
- Đảm bảo `TestEngineClose` (hoặc thêm mới) gọi eng.Close() KHÔNG panic double-free.

---

## FIX 2 — CompactJunk rewrite index MỖI boot dù không có gì rác. (churn #1)

**Bằng chứng:** `engine.go:~196` gọi `e.CompactJunk()` trong `New` mỗi lần khởi động.
`store.Compact` (`internal/store/file_store.go:~223`) LUÔN ghi đè `chunks.gob` (33MB) +
`dirs.json` và đánh số lại toàn bộ chunk ID từ 0 — kể cả khi `removed == 0`.

**VIỆC LÀM (chọn cách rẻ nhất, KHÔNG phá self-heal):**
- Trong `FileStore.Compact`: nếu sau khi lọc mà **không có chunk nào bị bỏ VÀ không có
  dir nào bị bỏ** (kept đúng bằng số chunk cũ, keptDirs đúng bằng số dir cũ), thì
  **KHÔNG gọi `fs.save()` / `fs.saveDirs()`** — trả kết quả nhưng bỏ qua ghi đĩa.
  → Self-heal vẫn dọn được khi THẬT SỰ có rác (lần đầu), nhưng boot sạch không ghi lại.
- LƯU Ý không đánh số lại ID nếu không cần: nếu không có gì bị bỏ, giữ nguyên ID cũ
  (đừng reassign từ 0 vô ích). Cách đơn giản: phát hiện "no-op" SỚM (đếm trước), nếu
  no-op thì return luôn không động vào fs.chunks/fs.index.

**TEST (internal/store/compact_noop_test.go):**
- `TestCompactNoopDoesNotRewrite`: tạo store tạm, thêm vài chunk sạch (không junk).
  Ghi nhận mtime của file chunks.gob. Gọi Compact với predicate keep-all. Assert:
  mtime KHÔNG đổi (file không bị ghi lại) VÀ chunk IDs giữ nguyên.
- `TestCompactStillRemovesJunk`: thêm chunk có path chứa "node_modules". Compact với
  predicate loại junk. Assert: chunk junk bị bỏ, file ĐƯỢC ghi lại (mtime đổi), số
  còn lại đúng.

---

## FIX 3 — Background home-indexing hard-code OnlyExtensions loại bỏ .md + code. (churn #2 + bug index)

**Bằng chứng:** `internal/webui/webui.go:~577`:
```go
opts.OnlyExtensions = []string{"pdf", "docx", "xlsx", "txt"}
```
Dòng này LOẠI BỎ `.md` và TẤT CẢ file code, bất kể reader registry hỗ trợ gì. Đây là
lý do file .md của repo không bao giờ vào index.

**VIỆC LÀM:**
- BỎ dòng `opts.OnlyExtensions = ...` đó đi (để rỗng = nhận MỌI ext mà reader registry
  hỗ trợ — registry giờ đã có .md + code sau fix reader).
- HOẶC nếu muốn giữ danh sách tường minh, đặt nó = đúng tập reader.Registry hỗ trợ
  (bao gồm .md và code). Đơn giản nhất: xoá dòng đó, để default.

**TEST:** không cần test riêng (đã có dedup_test.go cho index). Chỉ cần đảm bảo
`go build ./...` xanh và `go test ./internal/...` không hỏng. Nếu muốn: thêm assert
trong một test webui rằng background opts không ép OnlyExtensions.

---

## FIX 4 — (TUỲ CHỌN, làm nếu còn thời gian) Freshness: file mới trong dir đã-index không được nhặt.

**Bằng chứng:** `engine.go` indexThrottled skip file theo `existing[path]` (đã có trong
store). Nếu file MỚI xuất hiện trong một dir đã từng index, nó VẪN được nhặt (vì path
chưa có trong store) — cái này THỰC RA ĐÚNG. Vấn đề thật là dir đã nằm trong dirs.json
thì home-indexing skip cả dir (webui.go:551). 

**VIỆC LÀM (nhẹ nhàng, đừng phá chống-nhân-bản):**
- Không bắt buộc. Nếu làm: trong runBackgroundHomeIndexing, KHÔNG skip toàn bộ dir chỉ
  vì nó trong alreadyIndexed — thay vào đó luôn walk lại nhưng dựa per-file existing[path]
  (đã có sẵn) để bỏ file cũ. Nhưng cẩn thận: re-walk 1891 dir mỗi boot tốn CPU.
  → Nếu không chắc, BỎ QUA fix 4, chỉ ghi chú "freshness cần thiết kế lại riêng".

---

## QUY TRÌNH (KHÔNG nhảy bước)
- B1. Đọc engine.go func New + Close, file_store.go Compact, webui.go dòng ~577.
- B2. FIX 1 (share model) + test. Chạy `go test ./internal/engine/ -race`. Xanh.
- B3. FIX 2 (compact no-op) + test. Chạy `go test ./internal/store/`. Xanh.
- B4. FIX 3 (bỏ OnlyExtensions). Build + `go test ./internal/...`. Xanh.
- B5. (tuỳ chọn) FIX 4.
- B6. Toàn bộ `go build ./...` + `go vet ./...` + `go test ./internal/... -timeout 900s` xanh.

## LỆNH KIỂM THỬ
```bash
cd "/Users/tatlatat/Documents/level 3"
go build ./... 2>&1 | grep -v "duplicate librar"
go vet ./internal/engine/ ./internal/store/ ./internal/webui/
go test ./internal/engine/ -race -timeout 300s 2>&1 | grep -v "duplicate librar"
go test ./internal/store/ -timeout 120s 2>&1 | grep -v "duplicate librar"
go test ./internal/... -timeout 900s 2>&1 | grep -v "duplicate librar" | tail -15
```

## CHỐNG DRIFT
Mục tiêu DUY NHẤT: (1) engine.New nhanh hơn ~2x bằng cách load mỗi model 1 lần;
(2) boot sạch KHÔNG ghi lại index; (3) background index nhận .md + code. KHÔNG đổi
public API, KHÔNG đụng internal/index/, KHÔNG thêm dependency. Nếu thấy mình refactor
to hơn thế → DỪNG, quay lại đây.

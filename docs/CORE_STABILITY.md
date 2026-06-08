# Lõi ổn định — better-recoll

> Tài liệu CHỐT. Đọc trước mỗi lần đụng dự án. Mục đích: phân định rõ **cái gì
> chắc chắn (không đụng)**, **cái gì đang phải vá**, và tiêu chí **"khách mở máy
> lên là chạy OK"**. Để không lạc, không "tùy biến" lung tung mỗi lần khởi động.

Cập nhật: 2026-06-06.

---

## 0. NGUYÊN TẮC

1. **Lõi là bất biến.** Phần ở mục 1 đã được đo/chứng minh. KHÔNG sửa trừ khi có
   bằng chứng nó hỏng (test đỏ, số đo tụt). Không "cải thiện" mò.
2. **Mỗi thay đổi phải có bằng chứng**, không đoán. Số đo > cảm giác.
3. **Tiêu chí thành công của sản phẩm = mục 3** (khách mở máy lên OK). Mọi việc
   khác (hotkey, UI đẹp) là phụ, làm SAU khi mục 3 xanh.

---

## 1. 🟢 LÕI CHẮC CHẮN (đã chứng minh — KHÔNG đụng)

| Thành phần | Bằng chứng (đo được) | File chính |
|---|---|---|
| Embedder BGE-M3 + reranker int8 | parity cosine=1.0; search query thật score 0.97 | `internal/model/`, `internal/engine/rerank.go` |
| HNSW (≥50k vector) | recall vector THẬT=1.0; oracle khó pass (0.94/0.97); 1.5ms@200k | `internal/index/vector.go` |
| Flat-scan (<50k vector) | exact, recall 1.0 | `internal/index/vector.go` |
| Reader (doc + code) | search `.go`/`.md` ra đúng file; ~90 đuôi | `internal/reader/txt.go` |
| Index dedup + compact no-op | không churn (mtime không đổi qua restart) | `internal/store/file_store.go`, `internal/engine/engine.go` |
| Index batch (32, cap 64) | ~2-3x nhanh hơn batch 8 | `internal/engine/engine.go` |
| Web UI search | localhost:8765, phân tầng CHÍNH XÁC/GỢI Ý đúng | `internal/webui/` |
| 2 model riêng search/index | parallel-index + search-trong-lúc-index (commit 01917cd cố ý) | `internal/engine/engine.go` |

**Quy tắc lõi:** đụng vào đây phải chạy `go test ./internal/... ` (full, không -short)
và oracle recall PHẢI còn pass. Recall là vua.

---

## 2. 🔴 ĐANG PHẢI VÁ (gốc rễ của "mỗi lần khởi động lại tùy biến")

> Đây là LÝ DO mỗi lần mở dự án phải set env/sửa state. Phải làm chắc MỘT LẦN.

### V1. Model ở sai chỗ / setup để lại trạng thái hỏng  ✅ ĐÃ SỬA TẦNG CODE (2026-06-06)
Gốc rễ (đã tìm): `downloadFile` ghi THẲNG vào đích, mạng đứt = file cụt nằm ở đích
vĩnh viễn; `checkModelExists` chỉ kiểm mỗi `model.onnx` (file nhỏ) → tưởng OK dù
data cụt + tokenizer thiếu → app load fail với lỗi cryptic.

**Đã sửa (có test + verify e2e):**
- `verifyModelIntegrity(root)` — kiểm 6 file bắt buộc + kích thước tối thiểu
  (`requiredModelFiles` = nguồn sự thật). Bắt được data cụt + tokenizer thiếu.
- `checkModelExists` giờ dùng integrity (không chỉ model.onnx).
- `downloadFile` ATOMIC: tải `.part` → verify đủ byte (Content-Length) → rename.
  Mạng đứt chỉ để lại `.part`, KHÔNG bao giờ file cụt ở đích.
- `downloadAndSetupModels` verify cuối: tải xong mà không toàn vẹn → báo lỗi rõ.
- Khởi động: nếu hỏng → log RÕ file nào thiếu/cụt + `cleanCorruptModelFiles` tự
  dọn file cụt/.part (để setup sau tải sạch, không skip nhầm). KHÔNG tự tải 2.3GB.
- Tests: `internal/webui/integrity_test.go` (4 test, pass). E2e: chạy trỏ ~/.sfs
  hỏng → báo đúng 5 file + dọn data cụt.

**✅ MÁY NÀY ĐÃ FIX (2026-06-06):** copy model đầy đủ từ repo → `~/.sfs` (data
2.27GB + tokenizer 17MB, đủ 6 file). **PHÉP THỬ ĐẠT:** chạy `sfs-server` từ /tmp
KHÔNG set env nào → "Engine successfully initialized" → search API 200, time-to-
ready 3.29s. Tức app TỰ TÌM model ở ~/.sfs, KHÔNG cần SFS_MODELS_DIR/SFS_ROOT nữa.
→ Tiêu chí "khách mở máy lên chạy OK" (không tùy biến): **ĐẠT cho phần model.**

### V2. onboarded=true → tự nuốt home dir mỗi lần mở  ✅ ĐÃ SỬA (2026-06-06)
Gốc rễ (đã tìm): 2 chỗ tự gọi `runBackgroundHomeIndexing` (walk cả `os.UserHomeDir()`
→ ~1891 thư mục): (a) lúc khởi động nếu onboarded, (b) NGAY sau khi user onboard 1
thư mục (handleOnboard line ~734 "Starting background home indexing") → user chọn
1 dir, app tự lan ra nuốt cả home.

**Đã sửa:**
- Thêm `refreshIndexedDirs(eng)` — chỉ re-quét các thư mục user ĐÃ chọn
  (`eng.IndexedDirs()`), KHÔNG walk home. Per-file dedup lo file đã index.
- Thay 2 chỗ auto-call (startup + sau setup) bằng `refreshIndexedDirs`.
- Bỏ dòng tự-nuốt-home trong `handleOnboard` (user chọn dir nào → index dir đó).
- `runBackgroundHomeIndexing`/`findSafeTrees` giữ lại (dead code, đánh dấu rõ)
  phòng làm tính năng "index cả home" như lựa chọn user CHỦ ĐỘNG, không auto.

**PHÉP THỬ ĐẠT:** chạy server với state onboarded → log "refresh: cập nhật 40 thư
mục user đã chọn (không quét toàn home)", KHÔNG còn "Background indexing home
subdirectory". Trước: "Found 1891 safe directories". → Mở máy nhẹ, không giật.

### V3. Binary tạm ở /tmp / chưa có build chuẩn  ✅ ĐÃ SỬA (2026-06-06)
Gốc rễ: chưa có build system → tôi build vào /tmp (không bền, thiếu libs/ cạnh
binary) → phải set env. Thêm: clang/CGO `-L` vỡ vì đường dẫn repo có khoảng trắng
("level 3" → 'no such file: 3/libs').

**Đã làm:**
- `Makefile` (build đa nền): `make dist` → build 3 binary + copy libonnxruntime
  vào `dist/libs/` → **tự chứa**. Né khoảng trắng bằng symlink `$TMPDIR/sfs-libs`.
- `docs/BUILD.md` — checklist build macOS (chạy được) + Linux/Windows (port sau):
  ràng buộc CGO (build native mỗi OS), libtokenizers.a + libonnxruntime per-OS,
  code đã sẵn cross-platform (paths.go .so/.dll, build-tag darwin/fallback).
- `.gitignore` chặn `dist/`.

**PHÉP THỬ ĐẠT:** `dist/sfs-server` chạy từ /tmp KHÔNG env nào → "Engine
successfully initialized" + search `final` OK + 4.36s. Binary tự tìm lib
(dist/libs) + model (~/.sfs). → Hết phải tùy biến env.

**Cross-platform:** code sẵn sàng (paths.go đa OS, GUI tách build-tag). Để port
Win/Linux: build NATIVE trên OS đó + đặt libtokenizers.a + libonnxruntime của OS
đó vào libs/. Chi tiết trong docs/BUILD.md.

---

## 3. ✅ TIÊU CHÍ "KHÁCH MỞ MÁY LÊN LÀ CHẠY OK"

Sản phẩm coi là ĐẠT khi, trên một máy SẠCH (chưa từng tùy biến), làm đúng:

- [ ] Cài 1 lần (`sfs setup` hoặc mở .app) → model về đúng chỗ, kiểm toàn vẹn, xong.
- [ ] Mở app → KHÔNG cần set bất kỳ env nào (SFS_MODELS_DIR/SFS_ROOT) → engine
      init OK, không lỗi "failed to load tokenizer".
- [ ] Nếu model thiếu/hỏng → app BÁO RÕ + hướng dẫn, KHÔNG im lặng chạy lỗi.
- [ ] Mở app KHÔNG tự nuốt toàn bộ home dir; chỉ index khi user chọn.
- [ ] Search ra kết quả < 1s.
- [ ] Đóng/mở lại → KHÔNG churn index, KHÔNG nảy lỗi mới.

Khi 6 ô trên xanh trên máy sạch → lõi vận hành chỉnh chu. Lúc đó mới làm tính
năng phụ (mục 5).

---

## 4. CÁCH CHẠY ỔN ĐỊNH HIỆN TẠI (tạm, cho dev — KHÔNG phải sản phẩm cuối)

```bash
# Server web (đang dùng để kiểm thử). Cần env vì V1 chưa fix:
SFS_MODELS_DIR="/Users/tatlatat/Documents/level 3" \
SFS_ROOT="/Users/tatlatat/Documents/level 3" \
SFS_PORT=8765 /tmp/sfs-server-final
# → http://localhost:8765
```
Việc PHẢI set 2 env này = bằng chứng V1 chưa fix. Khi V1 xong, lệnh chạy không
cần env nào.

---

## 5. 🟡 TÍNH NĂNG PHỤ (làm SAU khi mục 3 xanh)

- Chế độ Spotlight (`cmd/sfs-app`): hotkey Ctrl+Option+Space + cửa sổ nổi. Hotkey
  FIRE được (đã test bằng app tối giản), nhưng toggle/floating còn lỗi hiển thị.
  GÁC LẠI.
- UI polish, dark mode, a11y.

---

## 6. ĐÃ ĐO ĐƯỢC (số thật, tham chiếu nhanh)

- embed BGE-M3 trên CPU: ~423ms/chunk (batch 64), ~870ms (batch 8). Index nặng.
- HNSW: recall@10 vector thật 1.0; oracle khó 0.94; 1.5ms@200k.
- engine.New (load 2 model ~2.9GB): 1.4–5.3s tùy disk page-cache.
- time-to-ready server: ~4–5s.
- search query thật: <1s, score 0.97 cho khớp tốt.

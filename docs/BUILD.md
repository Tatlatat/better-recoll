# Build đa nền tảng — better-recoll (macOS / Linux / Windows)

> Tài liệu để build + đóng gói trên từng hệ. Đọc trước khi port sang Win/Linux.

## Ràng buộc cốt lõi (đã xác minh, đừng quên)

1. **CGO BẮT BUỘC** — `onnxruntime_go` + `daulet/tokenizers` là CGO. ⇒ **KHÔNG
   cross-compile thuần.** Build macOS trên Mac, Linux trên Linux, Windows trên Win.
2. **2 thư viện C native per-OS:**
   - `libtokenizers.a` — static, link lúc build. Repo có bản **macOS arm64** ở `libs/`.
     Win/Linux cần `.a` tương ứng (tải từ release của `daulet/tokenizers` hoặc tự build Rust).
   - `libonnxruntime.{dylib/so/dll}` — load lúc **RUNTIME** (dlopen). Binary tự tìm
     trong `libs/` cạnh nó (`internal/model/paths.go` → `findOnnxRuntimeLib`).
3. **Khoảng trắng trong đường dẫn repo** (`level 3`) làm clang/CGO `-L` vỡ
   (`no such file: 3/libs`). Makefile tạo symlink không-khoảng-trắng (`$TMPDIR/sfs-libs`)
   để né. Nếu đặt repo ở đường dẫn KHÔNG khoảng trắng thì không cần mẹo này.
4. **Model** không nằm trong binary — `sfs setup` tải về `~/.sfs/models/` (xem
   docs/CORE_STABILITY.md V1). Binary tự tìm ở `~/.sfs` → `libs/` cạnh binary → cwd.

## Code đã sẵn sàng cross-platform

- `internal/model/paths.go` xử lý `.dylib/.so/.dll` + đường dẫn mac/linux/win.
- `daulet/tokenizers` có `#cgo windows/linux/darwin LDFLAGS` riêng — binding đa nền.
- GUI macOS-specific tách build-tag: `cmd/sfs-app/floating_darwin.go` (`//go:build
  darwin`) + `floating_fallback.go` (`//go:build !darwin`, stub rỗng cho Win/Linux).
  ⇒ `sfs-app` build được mọi OS; cửa sổ nổi/Spotlight chỉ hoạt động trên macOS,
  Win/Linux dùng webview thường (chưa có floating native — TODO khi port).

---

## macOS (đã chạy được)

```bash
brew install onnxruntime          # → /opt/homebrew/lib/libonnxruntime.dylib
make dist                         # build 3 binary + copy lib → dist/
dist/sfs setup                    # tải model về ~/.sfs (lần đầu)
dist/sfs-server                   # chạy, không cần env → http://localhost:8765
```
`libs/libtokenizers.a` (macOS arm64) đã có sẵn trong repo.

## Linux (port sau — checklist)

```bash
# 1. libtokenizers.a cho linux: tải từ github.com/daulet/tokenizers releases
#    (libtokenizers.linux-amd64.tar.gz) → giải nén libtokenizers.a vào libs/
# 2. libonnxruntime.so: tải onnxruntime release linux .tgz → libonnxruntime.so
#    đặt vào /usr/lib/ (hoặc sửa ORT_SRC trong Makefile) ; make sẽ copy vào dist/libs/
# 3. cài build-essential (gcc, g++) cho CGO
make dist                         # GOOS=linux tự nhận; Makefile chọn .so
dist/sfs setup && dist/sfs-server
```

## Windows (port sau — checklist)

```powershell
# 1. libtokenizers: bản windows (.lib/.a) từ daulet/tokenizers → libs/
# 2. onnxruntime.dll: từ onnxruntime release win → đặt cạnh binary trong dist\libs\
# 3. cần MinGW-w64 gcc (CGO trên Windows) hoặc MSVC toolchain
#    Makefile dùng cú pháp Unix → trên Win dùng Git Bash/WSL, hoặc viết build.ps1
go build -o dist\sfs.exe .\cmd\sfs
go build -o dist\sfs-server.exe .\cmd\sfs-server
# copy onnxruntime.dll → dist\libs\
dist\sfs.exe setup && dist\sfs-server.exe
```
Lưu ý: Makefile hiện viết cho shell Unix. Khi port Win, hoặc chạy qua Git Bash,
hoặc thêm `build.ps1` tương đương.

---

## Kiểm tra "tự chứa" (mọi OS)

Sau `make dist`, chạy binary **từ thư mục khác, không set env nào**:
```bash
cd /tmp && /path/to/dist/sfs-server
```
PHẢI thấy `Engine successfully initialized` + search trả kết quả. Nếu báo thiếu
lib → libonnxruntime chưa ở `dist/libs/`. Nếu báo thiếu model → chạy `sfs setup`.

**Đã verify trên macOS (2026-06-06):** dist/sfs-server chạy từ /tmp, không env,
engine init OK, search `final` OK, time-to-ready 4.36s.

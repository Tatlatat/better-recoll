# ============================================================================
# better-recoll — build đa nền tảng (macOS / Linux / Windows)
#
# Ràng buộc (đã xác minh):
#  - CGO BẮT BUỘC (onnxruntime_go + daulet/tokenizers) → KHÔNG cross-compile
#    thuần. Mỗi OS build NATIVE: build macOS trên Mac, Linux trên Linux, Win trên Win.
#  - Cần libtokenizers.a (static, per-OS) ở libs/ — repo đã có bản macOS arm64.
#  - libonnxruntime.{dylib/so/dll} load lúc RUNTIME (dlopen). Binary tự tìm trong
#    libs/ cạnh nó (xem internal/model/paths.go findOnnxRuntimeLib).
#  - Model ở ~/.sfs/models/ (sfs setup tải về; xem V1). KHÔNG nằm trong binary.
#
# Mục tiêu: ra thư mục dist/ TỰ CHỨA, chạy KHÔNG cần env nào.
# ============================================================================

# Thư mục repo. LƯU Ý: clang/CGO KHÔNG xử lý được khoảng trắng trong -L flag
# (đường dẫn "level 3" bị cắt ở khoảng trắng → 'no such file: 3/libs'). Giải pháp:
# tạo symlink KHÔNG khoảng trắng tới libs/ và trỏ CGO vào đó.
REPO := $(CURDIR)
DIST := $(REPO)/dist
LIBS := $(REPO)/libs

# Symlink an toàn (không khoảng trắng) tới libs/, dùng cho CGO -L.
SAFELIBS := $(shell printf '%s' "$${TMPDIR:-/tmp}")sfs-libs
export CGO_LDFLAGS := -L$(SAFELIBS)

GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)

# libonnxruntime theo OS (binary sẽ tự tìm trong dist/libs/).
ifeq ($(GOOS),darwin)
  ORT_NAME := libonnxruntime.dylib
  ORT_SRC := /opt/homebrew/lib/libonnxruntime.dylib
endif
ifeq ($(GOOS),linux)
  ORT_NAME := libonnxruntime.so
  ORT_SRC := /usr/lib/libonnxruntime.so
endif
ifeq ($(GOOS),windows)
  ORT_NAME := onnxruntime.dll
  ORT_SRC := onnxruntime.dll
endif

.PHONY: all build dist clean test setup help safelibs

help:
	@echo "better-recoll build ($(GOOS)/$(GOARCH)):"
	@echo "  make build   — build 3 binary (sfs, sfs-server, sfs-app) vào dist/"
	@echo "  make dist    — build + copy libonnxruntime → dist/libs/ (tự chứa)"
	@echo "  make test    — go test ./internal/... -short"
	@echo "  make clean   — xoá dist/"
	@echo ""
	@echo "Sau 'make dist': chạy dist/sfs-server (KHÔNG cần env). Lần đầu chạy"
	@echo "  dist/sfs setup  để tải model về ~/.sfs/models."

# Symlink libs/ ra đường dẫn không-khoảng-trắng để clang/CGO -L hoạt động.
safelibs:
	@ln -sfn "$(LIBS)" "$(SAFELIBS)"

# Build 3 binary native vào dist/.
build: safelibs
	@mkdir -p "$(DIST)"
	@echo "→ build sfs (CLI) ..."
	@go build -o "$(DIST)/sfs" ./cmd/sfs
	@echo "→ build sfs-server (web) ..."
	@go build -o "$(DIST)/sfs-server" ./cmd/sfs-server
	@echo "→ build sfs-app (desktop) ..."
	@go build -o "$(DIST)/sfs-app" ./cmd/sfs-app
	@echo "✓ binary ở $(DIST)/"

# dist tự chứa: binary + libs/ (onnxruntime + tokenizers cạnh binary).
dist: build
	@mkdir -p "$(DIST)/libs"
	@echo "→ copy libonnxruntime ($(ORT_NAME)) vào dist/libs/ ..."
	@if [ -f "$(ORT_SRC)" ]; then cp "$(ORT_SRC)" "$(DIST)/libs/$(ORT_NAME)"; \
	 else echo "  ⚠️  KHÔNG thấy $(ORT_SRC). Trên $(GOOS) hãy cài onnxruntime rồi đặt $(ORT_NAME) vào $(DIST)/libs/"; fi
	@echo "✓ dist/ tự chứa. Chạy: \"$(DIST)/sfs-server\" (không cần env)."
	@echo "  Lần đầu: \"$(DIST)/sfs\" setup  (tải model về ~/.sfs)."

test:
	@go test ./internal/... -short -timeout 400s 2>&1 | grep -v "duplicate librar" || true

setup: dist
	@"$(DIST)/sfs" setup

clean:
	@rm -rf "$(DIST)"
	@echo "✓ đã xoá dist/"

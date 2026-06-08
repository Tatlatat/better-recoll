#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT_DIR="${1:-$ROOT/dist/release}"
VERSION="${VERSION:-dev}"
VERSION="${VERSION//\//-}"

GOOS="$(go env GOOS)"
GOARCH="$(go env GOARCH)"
TARGET="${GOOS}-${GOARCH}"

DEPS_DIR="$ROOT/.release-deps/$TARGET"
STAGE_DIR="$OUT_DIR/stage/$TARGET"
ARCHIVE_BASENAME="better-recoll-${TARGET}-${VERSION}"

mkdir -p "$DEPS_DIR" "$STAGE_DIR/libs" "$OUT_DIR"
rm -rf "$STAGE_DIR"
mkdir -p "$STAGE_DIR/libs"

download() {
  local url="$1"
  local dest="$2"
  curl -fsSL "$url" -o "$dest"
}

case "$TARGET" in
  darwin-arm64)
    TOKENIZERS_URL="https://github.com/daulet/tokenizers/releases/download/v1.27.0/libtokenizers.darwin-arm64.tar.gz"
    ONNX_URL="https://github.com/microsoft/onnxruntime/releases/download/v1.23.2/onnxruntime-osx-arm64-1.23.2.tgz"
    ONNX_LIB="libonnxruntime.dylib"
    ONNX_GLOB="onnxruntime-osx-arm64-1.23.2/lib/${ONNX_LIB}"
    BINARIES=("sfs" "sfs-server" "sfs-app")
    ;;
  linux-amd64)
    TOKENIZERS_URL="https://github.com/daulet/tokenizers/releases/download/v1.27.0/libtokenizers.linux-amd64.tar.gz"
    ONNX_URL="https://github.com/microsoft/onnxruntime/releases/download/v1.23.2/onnxruntime-linux-x64-1.23.2.tgz"
    ONNX_LIB="libonnxruntime.so"
    ONNX_GLOB="onnxruntime-linux-x64-1.23.2/lib/${ONNX_LIB}"
    BINARIES=("sfs" "sfs-server")
    ;;
  *)
    echo "unsupported packaging target: $TARGET" >&2
    exit 1
    ;;
esac

TOKENIZERS_ARCHIVE="$DEPS_DIR/tokenizers.tar.gz"
ONNX_ARCHIVE="$DEPS_DIR/onnxruntime.tgz"

download "$TOKENIZERS_URL" "$TOKENIZERS_ARCHIVE"
tar -xzf "$TOKENIZERS_ARCHIVE" -C "$DEPS_DIR"

download "$ONNX_URL" "$ONNX_ARCHIVE"
tar -xzf "$ONNX_ARCHIVE" -C "$DEPS_DIR"
install -m 0644 "$DEPS_DIR/$ONNX_GLOB" "$STAGE_DIR/libs/$ONNX_LIB"
install -m 0644 "$ROOT/LICENSE" "$STAGE_DIR/LICENSE"
install -m 0644 "$ROOT/README.md" "$STAGE_DIR/README.md"

# CGO breaks on repo paths with spaces, so expose the dependency directory via a safe symlink.
SAFEDEPS="${TMPDIR:-/tmp}/better-recoll-deps-${TARGET}"
ln -sfn "$DEPS_DIR" "$SAFEDEPS"
export CGO_LDFLAGS="-L$SAFEDEPS"

for bin in "${BINARIES[@]}"; do
  go build -o "$STAGE_DIR/$bin" "./cmd/$bin"
done

cat > "$STAGE_DIR/RUN-FIRST.txt" <<EOF
better-recoll package: $TARGET

1. Run the first-time model download:
   ./sfs setup --light

2. Index a folder:
   ./sfs index /path/to/your/documents

3. Start the web app:
   ./sfs-server

Open http://localhost:8765 after the server starts.

Notes:
- Models are not bundled in this zip; they are downloaded into ~/.sfs on first run.
- The macOS package includes sfs-app. Linux currently ships CLI + web server only.
EOF

ARCHIVE_PATH="$OUT_DIR/${ARCHIVE_BASENAME}.zip"
rm -f "$ARCHIVE_PATH"
(cd "$STAGE_DIR" && zip -qry "$ARCHIVE_PATH" .)
echo "created $ARCHIVE_PATH"

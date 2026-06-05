# sfs — Tìm file theo nội dung (semantic file search)

Gõ một câu mô tả nội dung; máy trả về đúng file chứa nội dung đó — không cần nhớ tên file. Chạy **hoàn toàn trên máy** (không gửi tài liệu lên mạng), **song ngữ Việt–Anh** (kể cả không dấu / câu trộn), một lần tìm **dưới 1 giây**.

Viết bằng **Go**, model **BGE-M3** (embedding) + **bge-reranker-v2-m3** (cross-encoder) chạy local qua **ONNX**.

## Bốn ràng buộc cốt lõi
1. **Khớp đúng** — kết quả chắc nằm ô "Chính xác", kết quả mờ nằm ô "Gợi ý" (không lừa người dùng).
2. **Dưới 1 giây** mỗi lần tìm (van `RerankK` điều chỉnh theo sức máy).
3. **Song ngữ Việt–Anh** — không dấu ("bien ban nghiem thu"), trộn ("lấy file template báo giá"), chéo ngôn ngữ.
4. **Chạy local** — không mạng, không phí dịch vụ.

## Kiến trúc
```
CLI (cmd/sfs)  +  GUI Fyne (cmd/sfs-gui)
        │  cùng gọi
   internal/engine  (Index / Search / SearchRanked)
   ├─ reader     đọc pdf/docx/xlsx/txt (giữ dấu tiếng Việt)
   ├─ chunk      cắt đoạn (rune-aware, giữ offset)
   ├─ normalize  bỏ dấu + lowercase (Đ→d)
   ├─ dedupe     đánh cờ đoạn-khung-chung (template)
   ├─ index      BM25 (chữ) + vector flat-scan (nghĩa) + int8 + watcher
   ├─ search     router mềm (điểm cộng, không loại ai)
   └─ model      Embedder (BGE-M3) + Reranker (cross-encoder) qua ONNX
```
**Ba interface thay-thế-được:** `model.Embedder` (pha sau đổi sang GPU server không sửa app), `store.Store`, `reader.FileReader` (cắm OCR sau).

## Số đo đã verify
- **Parity Go↔Python**: vector embedding khớp cosine = **1.000000** trên 20 câu Việt-Anh (tokenizer Go khớp byte-by-byte → không recall-sai-âm-thầm).
- **Recall cross-encoder**: **1.00 (10/10)** trên bộ cặp xây-dựng khó (không dấu + chéo ngôn ngữ), ~128 ms/query.
- **Tốc độ tìm** (kho 200 file, CPU M4 Max, van `RerankK=5`): avg **889 ms**, p-max **986 ms** — dưới 1 giây.
- 9 package test xanh, `go vet` sạch.

## Yêu cầu
- Go 1.26+, macOS arm64 (đã test trên M4 Max).
- `libonnxruntime.dylib` (`brew install onnxruntime`).
- `libtokenizers.a` cho cgo: tải prebuilt từ
  `github.com/daulet/tokenizers/releases/download/v1.27.0/libtokenizers.darwin-aarch64.tar.gz`,
  giải nén vào `/opt/homebrew/lib/libtokenizers.a`.
- Model ONNX trong `models/onnx/bge-m3/` và `models/onnx/bge-reranker/`
  (bge-m3 ship sẵn ONNX trên HuggingFace; reranker export qua
  `optimum-cli export onnx --task text-classification`).

## Cài & chạy (kéo-thả-chạy, không cần cài Python/Qt/ollama)

Binary tự tìm `libonnxruntime` (trong `libs/` cạnh nó) và thư mục `models/` — **không cần `SFS_ROOT`, không cần biến môi trường**.

```bash
# Lần đầu: tải model tự động từ HuggingFace (một lệnh)
./sfs setup            # bản đầy đủ (chất lượng cao nhất)
./sfs setup --light    # bản int8 reranker (570MB thay 2.2GB) — cho máy yếu, recall vẫn 1.00

# Index một thư mục tài liệu
./sfs index /đường/dẫn/tài-liệu

# Tìm (in 2 ô CHÍNH XÁC / GỢI Ý) — hỗ trợ không dấu
./sfs search "bien ban nghiem thu"

# Hoặc GUI một-thanh-tìm
./sfs-gui
```

**Build từ nguồn:**
```bash
export CGO_LDFLAGS="-L/opt/homebrew/lib"     # nơi có libtokenizers.a (static-link)
go build -o sfs ./cmd/sfs
go build -o sfs-gui ./cmd/sfs-gui
```

**Đóng gói phân phối:** đặt `libonnxruntime.dylib` vào `libs/` cạnh binary, model vào `models/` (hoặc để `sfs setup` tải). Người dùng chỉ cần tải thư mục về và chạy.

## Van tốc độ (`RerankK`)
Số candidate đưa vào cross-encoder = đánh đổi giữa tốc độ và recall. Mặc định **5** (dưới 1s trên CPU M-series). Máy mạnh tăng (12+) cho recall cao hơn; máy yếu giảm. Hai lớp BM25+vector đã lọc trước nên top-5 gần như luôn chứa đáp án đúng.

## Trạng thái
6/6 wave hoàn thành: model+parity → module nền → store+index+engine → rerank+2 ô → dedupe+diff-embed+router+watcher → GUI+int8+tốc độ. Pipeline đầu-cuối chạy thật, đo được, dưới 1 giây.

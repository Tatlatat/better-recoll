# Thiết kế: Tìm file theo nội dung (semantic file search), bản Go

> Spec đóng từ brainstorm ngày 2026-06-05. Mọi quyết định mở đã được chốt. Đây là tài liệu để review trước khi viết implementation plan.

---

## 0. Một câu

Người dùng gõ một câu mô tả nội dung; máy trả về đúng file chứa nội dung đó, không cần nhớ tên file. Chạy hoàn toàn trên máy người dùng, song ngữ Việt–Anh (kể cả không dấu / câu trộn), một lần tìm xong dưới 1 giây.

---

## 1. Bốn ràng buộc cứng (mọi quyết định phục tùng)

1. **Khớp đúng.** Khi máy nói "đây là file của bạn" thì phải thật sự đúng. Thà trả ít / nói "không chắc" còn hơn trả sai mà tỏ ra chắc.
2. **Dưới 1 giây** cho toàn bộ một lần tìm, tính cả bước rerank cuối.
3. **Song ngữ Việt–Anh**, gồm câu trộn ("lấy cái file template báo giá") và không dấu ("bien ban nghiem thu").
4. **Chạy local**: không gửi tài liệu lên mạng, không trả phí dịch vụ. Quy mô mục tiêu tới ~trăm ngàn → triệu file/máy (công ty xây dựng).

---

## 2. Quyết định nền móng đã chốt

| Quyết định | Chốt | Lý do |
|---|---|---|
| **Ngôn ngữ app** | **Go** | Linh tính người dùng + xu thế; giải 2/3 nỗi lo: (1) đóng gói 1 binary kéo-thả-chạy không bắt cài runtime, (2) goroutine không-GIL → index song song thật ở quy mô triệu file. Người dùng chấp nhận rủi ro đi theo xu thế Go. |
| **Model** | BGE-M3 (embed + sparse) + bge-reranker-v2-m3 (rerank), chạy **trong Go qua ONNX** | Một họ model làm cả nghĩa/từ-khoá/rerank. Chạy qua `onnxruntime-go` (cgo, libonnxruntime.dylib đã có trên máy). |
| **Tốc độ dài hạn** | Tách tầng model sau interface, **không** khoá cứng | Model chạy bằng kernel (ONNX/Metal/CUDA), không bằng ngôn ngữ app. Tốc độ doanh nghiệp đến từ thay tầng model (pha sau: HTTP→TEI/Triton GPU), app Go không đổi 1 dòng. |
| **GUI** | **Fyne** (GUI thuần Go), tối giản: một thanh tìm + 2 danh sách | Một binary thật, không webview. GUI cố tình tối giản nên tránh điểm yếu layout của Fyne. |
| **Vỏ** | CLI (lõi để đo) + GUI Fyne, cả hai gọi **cùng engine** | Engine là thư viện Go độc lập; vỏ mỏng, không logic riêng. |
| **OCR file scan/ảnh** | Interface `FileReader`, **cắm ở pha 2** | Pha 1 không làm; kiến trúc để cắm sau không phải viết lại. |
| **Diff-embed** | Pha 1 embed **cả đoạn** (baseline); diff-embed là cờ bật thêm sau, đo recall trước/sau để chứng minh | Không cải tiến mò; phải có baseline để so. |
| **KHÔNG xây tháp vector (HNSW/FAISS)** | Bỏ | Quét phẳng đã đo: 800k vector = 22ms. Tốc độ KHÔNG phải nút thắt. Thêm chỉ là nợ kỹ thuật. Để dành nếu lên >vài triệu file. |

---

## 3. Sáu nguyên lý cốt lõi (giữ nguyên từ ý tưởng gốc) + 1 nguyên lý thêm

1. **Chỉ embed phần khác biệt, không embed cả file.** Tài liệu cùng mẫu (báo cáo T1, T2…) phần lớn là khung chung → embed cả file thì giống hệt nhau dưới mắt máy. Tách khung chung, chỉ embed phần làm file này khác file kia. *(Pha 1: embed cả đoạn làm baseline; bật diff-embed ở bước sau, đo trước/sau.)*
2. **Tìm khác biệt ở tầng chữ, không ở tầng vector.** Khi đã thành vector thì không "trừ phần chung lấy phần riêng" được. Dò trùng làm ở tầng chữ: vân tay (MinHash/SimHash) từng đoạn, đoạn lặp nhiều file = khung chung.
3. **Khung chung → nhãn loại tài liệu (template).** Phần khung lặp là dấu hiệu "đây là biên bản / hồ sơ nghiệm thu". Giữ nó làm **tín hiệu định tuyến mềm** (xem nguyên lý 7-sửa).
4. **Hai lớp bù nhau.** Lớp chữ chính xác (BM25/sparse — lo từ khoá đúng, kể cả từ trong khung chung) + lớp embed nghĩa (lo mô tả mơ hồ & chéo ngôn ngữ). Không lớp nào đủ một mình.
5. **Một bước rerank cuối.** Sau khi 2 lớp lọc còn ~K đoạn, một cross-encoder chấm lại từng đoạn so với query. Biến "gần đúng" thành "đúng". Chậm nên chỉ chạy trên K, không chạy cả kho.
6. **Hai ô kết quả: "Chính xác" và "Gợi ý".** Trên ngưỡng → ô Chính xác; dưới → ô Gợi ý (ghi rõ là gợi ý). Người dùng luôn có đáp án, không bị lừa rằng cái mờ là cái chắc. *(Đây là hiện thân của ràng buộc #1.)*

**+ Nguyên lý 7 (nâng từ phản biện — KHÔNG có trong bản gốc, nhưng là trung tâm):**
**Reranker là VAN điều tiết, không phải bước phụ.** K (số đoạn rerank) = hàm của sức máy, không phải hằng số. Reranker là cross-encoder, không cache được, nuốt 80-90% ngân sách 1 giây. Ràng buộc #1 (đúng) và #2 (<1s) kéo ngược nhau **đúng tại đây**. Máy mạnh K=30; máy yếu K=10. Đo trên máy yếu nhất nhắm tới *trước*.

---

## 4. Kiến trúc tầng & ba interface thay-thế-được

```
┌──────────────────────────────────────────────────┐
│ VỎ:  CLI  │  GUI Fyne (1 thanh tìm + 2 danh sách) │  ← mỏng, không logic
└───────────────┬──────────────────────────────────┘
                │ engine.Index(dir) / engine.Search(query)
┌───────────────▼──────────────────────────────────┐
│ ENGINE (thư viện Go độc lập)                      │
│  Pha A index: FileReader→Chunker→Normalizer→      │
│    DedupeFinder→Embedder.Embed→Store              │
│  Pha B search: Router(mềm)→[BM25 ∥ flat-scan]→    │
│    Embedder.Rerank (VAN)→chia ô Chính xác/Gợi ý   │
└──┬──────────────┬──────────────┬─────────────────┘
   │ Embedder     │ Store        │ FileReader
┌──▼─────────┐ ┌──▼─────────┐ ┌──▼──────────────┐
│ MODEL(thay)│ │ LƯU TRỮ    │ │ ĐỌC FILE (thay) │
│ pha1: ONNX │ │ vector+đoạn│ │ pha1: pdf/docx/ │
│  -go local │ │ +template  │ │   xlsx          │
│ phaN: HTTP │ │ +2 chỉ mục │ │ pha2: + OCR     │
│  →TEI GPU  │ │ trên đĩa   │ │   ảnh/scan      │
└────────────┘ └────────────┘ └─────────────────┘
```

**Ba điểm thay-thế-được** (đúng 3 chỗ người dùng nói sẽ thay đổi):

| Interface (Go) | Hợp đồng | Pha 1 | Pha sau |
|---|---|---|---|
| `Embedder` | `Embed([]string) [][]float32`; `Rerank(q string, []string) []float32` | onnxruntime-go local (Metal) | HTTP client → TEI/Triton Rust-GPU; app không đổi |
| `Store` | ghi/đọc vector + đoạn + cờ-khung-chung + template + 2 chỉ mục | file local (mmap) | DB nhúng khi quy mô lớn |
| `FileReader` | `Read(path) (text string, err)`, đăng ký theo đuôi file | pdf-text/docx/xlsx | + OCR ảnh/scan (Tesseract/Paddle) |

Mỗi module test riêng được: FileReader, Chunker, Normalizer, DedupeFinder, Router, Embedder, chia-ô.

---

## 5. Luồng dữ liệu

### Pha A — Index (nặng, một lần; goroutine song song)
```
thư mục → [FileReader] text thô
       → [Chunker] đoạn + offset (để rerank đọc đúng đoạn, không đọc cả file)
       → [Normalizer] mỗi đoạn 2 bản: có-dấu / bỏ-dấu-thường
       → [DedupeFinder] vân tay → gom cụm TRƯỚC rồi so trong cụm (tránh O(n²))
                       → đánh cờ đoạn-khung-chung + dựng bảng template
       → [Embedder.Embed] PHA 1: embed MỌI đoạn (baseline)
       → [Store.Write] vector + đoạn + cờ + template + 2 chỉ mục (BM25 + vector flat)
```

### Pha B — Search (nhẹ, mỗi lần gõ, <1s)
```
query → [Normalizer] (cùng bộ pha A) → 2 bản
     → [Router] điểm-CỘNG-mềm theo template (KHÔNG loại ai — sửa từ phản biện)
     → song song: [BM25/sparse] ∥ [Embedder.Embed(query)→Store flat-scan]
     → hợp nhất → top-K ĐOẠN (K = van, mặc định 20)
     → [Embedder.Rerank] K đoạn  ← VAN, K co theo sức máy
     → ngưỡng → ô "Chính xác" (trên) / ô "Gợi ý" (dưới)
```

---

## 6. Hai ràng buộc vận hành (nâng từ phản biện thành điều khoản)

- **Van reranker.** `K = f(hardware)`. Đo thời gian rerank trên **máy yếu nhất nhắm tới** trước khi chốt K mặc định. Speed không bao giờ được đánh đổi bằng false-miss (recall).
- **RAM ở quy mô lớn.** 100k file × ~8 đoạn × 1024-dim × f32 ≈ 3.3GB chỉ vector. Máy dev (137GB) không lo; **máy nhân viên 8-16GB cần int8-quantize vector** (giảm 4× → ~0.8GB), bật khi đóng gói. int8 gần như miễn phí độ chính xác ở bước thô vì reranker dọn lại.

---

## 7. Xử lý song ngữ / tiếng Việt

- **Tầng chữ** (không dấu, gõ tắt, hoa/thường, từ trộn): Normalizer + lớp BM25. Việc cơ học, làm một lần lúc index.
- **Tầng nghĩa** (file tiếng Anh ↔ query tiếng Việt cùng nghĩa): chỉ lớp embed cứu được; BGE-M3 học không gian nghĩa chung đa ngôn ngữ. Câu trộn "lấy cái file template" → "file"/"template" là token tiếng Anh model vốn biết, không cần dịch.

---

## 8. Rủi ro & cách gác cổng

| Rủi ro | Mức | Gác cổng |
|---|---|---|
| **Tokenizer Go lệch Python** → vector lệch → recall sai **âm thầm** | **CAO** | **Cổng parity ở Bước 0**: so vector 20 câu mẫu Go vs Python tham chiếu, phải khớp ~1e-4. Lệch → sửa tokenizer trước khi làm gì. |
| BGE-M3 không kham câu trộn Việt-Anh | CAO | **Cổng sống-còn ở Bước 0**: 30-50 cặp (query↔đoạn) gồm ca khó + ca bẫy, đo recall. |
| Reranker vỡ ngân sách 1s trên CPU máy yếu | TRUNG | Van K co theo máy (nguyên lý 7); đo trên máy yếu trước. |
| RAM phình ở triệu file | TRUNG | int8 (mục 6); gom cụm dedupe tránh O(n²). |
| Router cứng loại oan kết quả đúng (không dấu/đồng nghĩa) | TRUNG | Router là tín hiệu MỀM (điểm cộng), không bao giờ loại ai. |
| onnxruntime-go là cgo → binary cần .dylib kèm | THẤP | Chấp nhận: "1 binary + 1 lib". Doc rõ. |
| File mới đổi "phần khác biệt" của file cũ | THẤP | Diff-embed khó cập nhật tăng dần → pha 1 dùng baseline (embed cả đoạn) dễ index lại từng file. |

---

## 9. Dữ liệu lưu trên máy (sau pha index)
Đoạn chữ đã cắt (gốc + chuẩn hoá) · vector mỗi đoạn → trỏ file gốc · bảng template (loại + khung chung) · 2 chỉ mục (BM25 + vector flat). Tất cả trong một thư mục data của app; xoá đi = index lại từ đầu.

---

## 10. Lộ trình dựng (rủi ro cao trước)

| # | Việc | Cổng/Ghi chú |
|---|---|---|
| **0** | **Bước 0 hai cổng (Go-ONNX):** export BGE-M3 + reranker → ONNX; chạy trong Go; (a) **cổng parity** vector Go≈Python; (b) **cổng sống-còn** 30-50 cặp Việt-Anh-không-dấu + ca bẫy, đo recall + thời-gian-rerank trên M4 Max/Metal | **Cả hai cổng xanh mới đi tiếp.** Đây là cổng sinh-tử. |
| 1 | Engine tối giản đầu-cuối: FileReader(pdf/docx/xlsx)→Chunker→Embed-cả-đoạn→flat-scan→trả về (chạy đúng trên vài trăm file thật) | baseline |
| 2 | Reranker (van) + chia ô Chính xác/Gợi ý → đo độ đúng | — |
| 3 | Dedupe + diff-embed (cờ) → so recall trước/sau (chứng minh nguyên lý 1 ăn tiền) | — |
| 4 | Router mềm + watcher canh file mới (index lại từng file) | — |
| 5 | CLI hoàn chỉnh + GUI Fyne một-thanh-tìm | — |
| 6 | int8 + đo tốc độ kho lớn (vài chục→trăm ngàn file), siết <1s, van K theo máy | — |

---

## 11. Môi trường đã xác minh (máy dev)
Go 1.26.3 (arm64) · libonnxruntime.1.26.0.dylib (brew) · Python onnxruntime 1.26 (để export ONNX) · torch 2.12 + MPS available · M4 Max + 137GB RAM · agy (Antigravity CLI) sống trong tmux `work`.

---

## 12. Những chỗ còn mở (không chặn Bước 0, chốt khi tới)
- Tên dự án (chưa đặt).
- Bộ đọc Go cho pdf/docx/xlsx tiếng Việt: chọn thư viện cụ thể ở Bước 1 (ledongthuc/pdf, unidoc, hoặc gọi ngoài) — test với file có dấu.
- Hugot vs (onnxruntime-go + tokenizers) rời: thử hugot trước ở Bước 0 (gói sẵn), rớt thì dùng 2 lib rời.

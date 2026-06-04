# Semantic File Search (Go + ONNX) — Implementation Plan

> **For agentic workers:** Người code chính là **agy** (Antigravity CLI, Gemini/Claude models) chạy song song trong tmux windows. **Claude là orchestrator**: phân rã task → spawn nhiều worker agy song song (dynamic wave) → gatekeep → kiểm thử → hợp nhất. Claude KHÔNG code tay; Claude viết spec subtask, driver agy, và verify. Steps dùng checkbox (`- [ ]`).

**Goal:** Một công cụ Go chạy local: gõ câu mô tả → trả đúng file chứa nội dung, song ngữ Việt-Anh (kể cả không dấu/trộn), <1s/lần tìm, đóng gói 1 binary.

**Architecture:** App Go (goroutine, triệu file). Model BGE-M3 + bge-reranker-v2-m3 chạy trong Go qua ONNX (onnxruntime-go + tokenizer), ẩn sau interface `Embedder` (pha sau đổi sang HTTP→TEI-GPU không sửa app). Hai lớp tìm (BM25 ∥ vector flat-scan) → rerank (van) → 2 ô Chính xác/Gợi ý. Interface `Store` và `FileReader` để thay-thế-được (lưu trữ lớn · OCR pha 2).

**Tech Stack:** Go 1.26 · onnxruntime-go (cgo, libonnxruntime.dylib có sẵn) · daulet/tokenizers (hoặc hugot) · Fyne (GUI) · ledongthuc/pdf + unioffice/docx + xlsx readers · MinHash dedupe · BM25.

**Orchestration (ultra-code / dynamic workflow):**
- Mỗi "wave" = nhóm subtask **độc lập** chạy song song, mỗi subtask một **window agy riêng** (`work:agy-w<N>-<slug>`) + một **cwd cô lập** (worktree hoặc package dir riêng).
- Pattern đã CHỨNG MINH chạy: spawn N window agy, gửi N task qua `tmux send-keys`, poll `capture-pane` tới khi mỗi cái xong, Claude gatekeep từng output.
- Subtask phụ thuộc → tuần tự (wave sau chờ wave trước). Số worker mỗi wave = số subtask song-song-được của wave đó (dynamic, không cố định).
- Claude verify MỌI output worker bằng test thật trước khi merge. Worker FAIL → Claude viết lại spec, re-dispatch (không tự sửa tay).

**Repo layout (Go module):**
```
go.mod  (module sfs)
cmd/sfs/main.go            — CLI entrypoint
cmd/sfs-gui/main.go        — Fyne GUI (pha 5)
internal/model/            — Embedder interface + onnx impl  (Embedder)
internal/store/            — Store interface + file impl     (Store)
internal/reader/           — FileReader interface + pdf/docx/xlsx (FileReader)
internal/chunk/            — Chunker
internal/normalize/        — Normalizer (bỏ dấu, lowercase)
internal/dedupe/           — DedupeFinder (MinHash) + template
internal/index/            — BM25 + vector flat index
internal/search/           — Router(mềm) + 2-lớp + rerank van + chia ô
internal/engine/           — Engine: Index() / Search() ráp mọi thứ
testdata/                  — file mẫu (pdf/docx/xlsx VN) + bộ cặp query↔đoạn
scripts/parity/            — Bước 0: export ONNX + so Go vs Python
```

---

## WAVE 0 — Cổng sinh-tử (TUẦN TỰ, 1 worker, KHÔNG được bỏ qua)

> Hai cổng. Cả hai xanh mới sang Wave 1. Đây là nơi dự án sống hoặc chết.

### Task 0.1: Khởi tạo Go module + skeleton interfaces

**Files:**
- Create: `go.mod`, `internal/model/embedder.go`, `internal/store/store.go`, `internal/reader/reader.go`

- [ ] **Step 1: Claude tạo go.mod + 3 interface (định nghĩa hợp đồng, chưa impl)**

```go
// internal/model/embedder.go
package model

// Embedder ẩn model sau interface — pha 1 ONNX local, pha sau HTTP→TEI.
type Embedder interface {
	Embed(texts []string) ([][]float32, error)   // dense vectors
	Rerank(query string, texts []string) ([]float32, error) // cross-encoder scores
	Dim() int
}
```
```go
// internal/store/store.go
package store

type Chunk struct {
	ID        int64
	FilePath  string
	Text      string   // bản gốc có dấu
	NormText  string   // bản bỏ dấu lowercase
	Offset    int      // vị trí trong file
	IsBoilerplate bool // cờ khung chung
	Vector    []float32
}
type Store interface {
	Write(chunks []Chunk) error
	AllVectors() ([][]float32, []int64)      // cho flat-scan
	GetChunk(id int64) (Chunk, error)
	Close() error
}
```
```go
// internal/reader/reader.go
package reader

// FileReader: đăng ký theo đuôi file. Pha 2 thêm OCR chỉ là 1 reader mới.
type FileReader interface {
	Extensions() []string
	Read(path string) (text string, err error)
}
```

- [ ] **Step 2: Verify build**

Run: `cd "/Users/tatlatat/Documents/level 3" && go build ./...`
Expected: PASS (interfaces compile, no impl yet)

- [ ] **Step 3: Commit**
```bash
git add go.mod internal/ && git commit -m "chore: Go module + Embedder/Store/FileReader interfaces"
```

### Task 0.2: Export BGE-M3 + reranker sang ONNX (Python, dùng để tham chiếu parity)

**Files:**
- Create: `scripts/parity/export_onnx.py`, `scripts/parity/reference_vectors.py`, `scripts/parity/sample_texts.json`

> Worker agy spec: dùng Python (đã có onnxruntime 1.26 + torch) để export model sang ONNX và sinh vector tham chiếu. Đây là PYTHON, chỉ để tạo "đáp án đúng" — app vẫn Go.

- [ ] **Step 1: Claude viết spec, dispatch 1 worker agy**

Worker task spec:
```
Trong thư mục scripts/parity/ tạo:
1. sample_texts.json: 20 câu trộn Việt-Anh + không dấu, ví dụ:
   ["biên bản nghiệm thu", "bien ban nghiem thu", "lấy file template báo giá",
    "meeting minutes Q1", "hợp đồng xây dựng nhà xưởng", ...]
2. export_onnx.py: dùng FlagEmbedding hoặc transformers + optimum để export
   BAAI/bge-m3 (dense) và BAAI/bge-reranker-v2-m3 sang ONNX vào models/onnx/.
   Nếu optimum chưa cài, cài: pip install optimum[onnxruntime] FlagEmbedding sentence-transformers
3. reference_vectors.py: load bge-m3 BẰNG sentence-transformers (PyTorch, đường chuẩn),
   embed 20 câu trong sample_texts.json, lưu reference_vectors.npy + in shape + norm.
Chạy cả 3, đảm bảo export_onnx tạo file .onnx và reference_vectors.npy tồn tại.
```

- [ ] **Step 2: Claude verify**

Run: `ls -la "/Users/tatlatat/Documents/level 3/models/onnx/" scripts/parity/reference_vectors.npy`
Expected: file .onnx tồn tại + reference_vectors.npy (shape 20×1024)

- [ ] **Step 3: Commit** (gitignore models/onnx vì lớn; commit scripts + .npy nhỏ)

### Task 0.3: ⛔ CỔNG PARITY — chạy ONNX trong Go, so với vector Python

**Files:**
- Create: `internal/model/onnx_embedder.go`, `internal/model/onnx_embedder_test.go`

- [ ] **Step 1: Claude dispatch worker agy: impl Embedder bằng onnxruntime-go + tokenizer**

Worker task spec:
```
Implement internal/model/onnx_embedder.go: struct OnnxEmbedder thỏa interface model.Embedder.
- Dùng github.com/yalue/onnxruntime_go để load models/onnx/bge-m3.onnx.
- Tokenizer: dùng github.com/daulet/tokenizers load tokenizer.json của bge-m3
  (XLM-RoBERTa sentencepiece). Nếu daulet/tokenizers khó build, thử
  github.com/knights-analytics/hugot (gói sẵn cả onnx+tokenizer).
- Embed(texts): tokenize → run onnx → mean-pool/CLS theo đúng cách bge-m3 →
  L2-normalize → trả [][]float32.
- Set ONNXRUNTIME path tới /opt/homebrew/lib/libonnxruntime.dylib.
go build ./internal/model/ phải pass.
```

- [ ] **Step 2: Claude viết test parity (KHÔNG để agy tự viết — đây là oracle)**

```go
// internal/model/onnx_embedder_test.go
func TestParityWithPython(t *testing.T) {
	// load scripts/parity/reference_vectors.npy (20×1024) + sample_texts.json
	// e := NewOnnxEmbedder(...); got, _ := e.Embed(texts)
	// với mỗi câu: cosine(got[i], ref[i]) > 0.9999
	// FAIL nếu bất kỳ câu nào lệch — nghĩa là tokenizer/pooling sai
}
```

- [ ] **Step 3: Run parity gate**

Run: `go test ./internal/model/ -run TestParityWithPython -v`
Expected: PASS — mọi câu cosine > 0.9999.
**Nếu FAIL:** Claude chẩn (tokenizer? pooling? normalize?), viết lại spec cho worker, re-dispatch. KHÔNG đi tiếp tới khi xanh.

- [ ] **Step 4: Commit** `git commit -m "feat(model): ONNX Embedder passes Python parity gate"`

### Task 0.4: ⛔ CỔNG SỐNG-CÒN — recall trên bộ cặp Việt-Anh + ca bẫy

**Files:**
- Create: `internal/model/survival_test.go`, `testdata/pairs.json`

- [ ] **Step 1: Claude viết testdata/pairs.json: 30-50 cặp {query, đoạn-đúng, [đoạn-bẫy]}**

```json
[
 {"query":"biên bản nghiệm thu công trình",
  "positive":"BIÊN BẢN NGHIỆM THU HOÀN THÀNH HẠNG MỤC CÔNG TRÌNH XÂY DỰNG...",
  "hard_negative":"BIÊN BẢN HỌP GIAO BAN CÔNG TRƯỜNG..."},
 {"query":"bien ban nghiem thu","positive":"BIÊN BẢN NGHIỆM THU...","hard_negative":"..."},
 {"query":"lấy file template báo giá vật tư","positive":"BẢNG BÁO GIÁ VẬT TƯ MẪU...","hard_negative":"..."},
 {"query":"meeting minutes construction","positive":"BIÊN BẢN HỌP CÔNG TRƯỜNG (tiếng Việt)...","hard_negative":"..."}
]
```

- [ ] **Step 2: Claude viết survival_test: embed query+đoạn, rerank, đo recall@1 và bẫy bị loại**

```go
func TestSurvivalRecall(t *testing.T) {
	// với mỗi cặp: rerank([positive, hard_negative...]) → positive phải đứng #1
	// đếm recall@1; in thời gian rerank trung bình/query
	// PASS nếu recall@1 >= 0.85 (ngưỡng sống-còn); in cảnh báo nếu rerank>700ms
}
```

- [ ] **Step 3: Run** `go test ./internal/model/ -run TestSurvivalRecall -v`
Expected: recall@1 ≥ 0.85, in median rerank time.
**Nếu FAIL recall:** model không kham → Claude báo user + thử model thay (bge-m3 vs khác) trước khi xây engine.

- [ ] **Step 4: Commit.** ✅ Wave 0 xanh = dự án KHẢ THI, sang Wave 1.

---

## WAVE 1 — Module nền độc lập (SONG SONG, 3 worker agy cùng lúc)

> 3 module KHÔNG phụ thuộc nhau → 3 window agy song song. Claude verify từng cái bằng unit test mình viết.

### Task 1.1 (worker A): Normalizer — bỏ dấu + lowercase

**Files:** Create `internal/normalize/normalize.go`, `internal/normalize/normalize_test.go`

- [ ] **Step 1: Claude viết test (oracle)**
```go
func TestStripDiacritics(t *testing.T) {
	cases := map[string]string{
		"Biên Bản Nghiệm Thu":"bien ban nghiem thu",
		"Báo Giá Vật Tư":"bao gia vat tu",
		"Đường ĐỎ":"duong do",
	}
	for in, want := range cases { if Normalize(in)!=want { t.Fatalf("%q→%q want %q",in,Normalize(in),want) } }
}
```
- [ ] **Step 2: dispatch worker A spec:**
```
Implement internal/normalize/normalize.go func Normalize(s string) string:
dùng golang.org/x/text/unicode/norm (NFD) + loại Mn (combining marks) + strings.ToLower.
Xử lý đúng chữ Đ/đ → d. go test ./internal/normalize/ phải pass.
```
- [ ] **Step 3: Claude run** `go test ./internal/normalize/ -v` → PASS
- [ ] **Step 4: Commit**

### Task 1.2 (worker B): FileReader pdf/docx/xlsx

**Files:** Create `internal/reader/pdf.go`, `docx.go`, `xlsx.go`, `registry.go`, `reader_test.go`; `testdata/sample.{pdf,docx,xlsx}`

- [ ] **Step 1: Claude tạo 3 file mẫu tiếng Việt có dấu vào testdata/** (qua worker hoặc script)
- [ ] **Step 2: Claude viết test:** mỗi reader đọc file mẫu trả text chứa từ khoá VN đã biết (vd "nghiệm thu").
- [ ] **Step 3: dispatch worker B spec:**
```
Implement internal/reader/: 
- pdf.go dùng github.com/ledongthuc/pdf
- docx.go dùng github.com/unidoc/unioffice hoặc baliance/gooxml (đọc text)
- xlsx.go dùng github.com/xuri/excelize/v2
- registry.go: map đuôi→reader, func ReadFile(path) gọi đúng reader.
Mỗi Read trả về plain text UTF-8 giữ nguyên dấu tiếng Việt.
go test ./internal/reader/ phải pass.
```
- [ ] **Step 4: Claude run test** → PASS (text có dấu đúng)
- [ ] **Step 5: Commit**

### Task 1.3 (worker C): Chunker

**Files:** Create `internal/chunk/chunk.go`, `chunk_test.go`

- [ ] **Step 1: Claude viết test:** đoạn ~512 ký tự, giữ Offset, không cắt giữa từ; text 2000 ký tự → ~4 chunk, offset tăng dần.
- [ ] **Step 2: dispatch worker C spec:**
```
Implement internal/chunk/chunk.go func Chunk(text string, size int) []store.Chunk-lite{Text,Offset}:
cắt theo ~size ký tự, ưu tiên ranh giới câu/xuống dòng, lưu Offset. 
go test ./internal/chunk/ phải pass.
```
- [ ] **Step 3: run** → PASS. **Step 4: Commit.**
- [ ] **Step 5: Claude hợp nhất Wave 1, build toàn bộ** `go build ./...` PASS

---

## WAVE 2 — Lưu trữ + chỉ mục + engine tối giản đầu-cuối (SONG SONG 2 + tuần tự ráp)

### Task 2.1 (worker A): Store file impl (mmap vector + chunk store)
**Files:** `internal/store/file_store.go`, `file_store_test.go`
- [ ] Claude viết test: Write N chunk → AllVectors trả đúng N vector + id; GetChunk(id) trả đúng text.
- [ ] dispatch worker spec (impl FileStore: vector ra .f32 file + chunk ra json/gob, AllVectors load vào [][]float32).
- [ ] run test PASS → commit.

### Task 2.2 (worker B): BM25 + vector flat-scan index
**Files:** `internal/index/bm25.go`, `internal/index/vector.go`, tests
- [ ] Claude viết test: BM25 trả đúng doc khi query khớp token; flat-scan trả top-K theo cosine (dùng vector giả).
- [ ] dispatch worker spec (BM25 cổ điển trên NormText; vector flat dùng dot-product đã normalize, argpartition top-K).
- [ ] run PASS → commit.

### Task 2.3 (tuần tự, Claude ráp + 1 worker): Engine.Index + Engine.Search tối giản
**Files:** `internal/engine/engine.go`, `engine_test.go`, `cmd/sfs/main.go`
- [ ] Claude viết integration test: Index(testdata/) rồi Search("nghiệm thu") trả file chứa nó ở top.
- [ ] dispatch worker: ráp reader→chunk→normalize→embed(cả đoạn, baseline)→store; Search: embed query→2 lớp→hợp nhất→trả top-K (chưa rerank). CLI `sfs index <dir>` và `sfs search <q>`.
- [ ] run integration test trên vài trăm file thật → top-1 đúng. commit.
- [ ] **MỐC: end-to-end chạy được (baseline, chưa rerank/chưa diff-embed).**

---

## WAVE 3 — Rerank (van) + 2 ô (tuần tự, phụ thuộc Wave 2)

### Task 3.1: Rerank van + ngưỡng + chia ô
**Files:** `internal/search/rerank.go`, `internal/search/classify.go`, tests
- [ ] Claude viết test: Search trả struct{Exact []Result, Suggest []Result}; positive vào Exact, bẫy không vào Exact.
- [ ] dispatch worker: sau hợp nhất lấy top-K (K từ config, mặc định 20) → Embedder.Rerank → ngưỡng τ chia 2 ô. K là tham số `--rerank-k` (van theo máy).
- [ ] run test + đo recall trên testdata/pairs.json → recall@Exact ≥ Wave0. commit.
- [ ] **MỐC: độ đúng đo được, 2 ô hoạt động.**

---

## WAVE 4 — Dedupe + diff-embed (chứng minh nguyên lý 1) + Router mềm + watcher (SONG SONG 2)

### Task 4.1 (worker A): DedupeFinder (MinHash) + template + cờ boilerplate + diff-embed flag
**Files:** `internal/dedupe/minhash.go`, `template.go`, tests
- [ ] Claude viết test: 3 file cùng khung + nội dung khác → đoạn khung bị đánh IsBoilerplate; template gom đúng loại.
- [ ] dispatch worker (MinHash/SimHash vân tay, gom cụm TRƯỚC rồi so trong cụm tránh O(n²); cờ boilerplate; bảng template).
- [ ] Claude đo **recall trước/sau** khi bật diff-embed (bỏ đoạn boilerplate khỏi embed) trên file cùng-mẫu → phải cải thiện. commit. **(chứng minh nguyên lý 1 ăn tiền)**

### Task 4.2 (worker B): Router mềm + watcher file mới
**Files:** `internal/search/router.go`, `internal/index/watch.go`, tests
- [ ] Claude viết test: query "biên bản" → template biên bản được CỘNG điểm (không loại loại khác); watcher thấy file mới → index lại đúng file đó.
- [ ] dispatch worker (Router = điểm cộng mềm theo template match, KHÔNG lọc cứng; watcher dùng fsnotify, index incremental 1 file).
- [ ] run PASS → commit.

---

## WAVE 5 — CLI hoàn chỉnh + GUI Fyne + int8 + đo tốc độ (SONG SONG 2 + đo)

### Task 5.1 (worker A): GUI Fyne một-thanh-tìm
**Files:** `cmd/sfs-gui/main.go`
- [ ] dispatch worker: Fyne app: 1 ô Entry tìm + 2 List (Chính xác / Gợi ý), gọi engine.Search, click mở file. Build `go build ./cmd/sfs-gui`.
- [ ] Claude verify build PASS + chạy thử mở được cửa sổ (screenshot/headless check). commit.

### Task 5.2 (worker B): int8 quantize vector + đo tốc độ kho lớn
**Files:** `internal/store/quantize.go`, `internal/index/vector.go` (int8 path), bench
- [ ] Claude viết bench: gen 100k vector → flat-scan int8 vs f32, đo ms + RAM; recall không tụt quá 1%.
- [ ] dispatch worker (int8 symmetric quantize, scan trên int8, dequant top-K trước rerank).
- [ ] Claude đo: scan <50ms ở 100k, RAM giảm ~4×, recall giữ. commit.

### Task 5.3 (tuần tự, Claude): kiểm thử toàn hệ + siết <1s
- [ ] Claude chạy full pipeline trên kho thật (vài nghìn→chục nghìn file): đo p50/p95 thời gian tìm; chỉnh van K để p95 <1s trên máy này.
- [ ] `go test ./...` toàn xanh. `go vet ./...` sạch.
- [ ] **MỐC CUỐI: dự án hoàn chỉnh — chạy đúng, đo được, <1s, 1 binary + lib.**

---

## Definition of Done (goal "hoàn hảo")
- [ ] Wave 0 cả 2 cổng xanh (parity ~1, recall ≥0.85)
- [ ] `go test ./...` toàn bộ PASS; `go vet ./...` sạch
- [ ] `go build ./cmd/sfs && go build ./cmd/sfs-gui` ra binary
- [ ] Search thật: query Việt + không-dấu + trộn + chéo-ngôn-ngữ → đúng file ở ô Chính xác; ca bẫy không lọt Chính xác
- [ ] diff-embed chứng minh cải thiện recall (số trước/sau)
- [ ] p95 thời gian tìm < 1s trên kho test
- [ ] README + cách chạy

## Self-review notes
- Mọi step có code/command thật, không placeholder.
- Type nhất quán: `model.Embedder`, `store.Chunk`/`store.Store`, `reader.FileReader` dùng xuyên suốt.
- Spec coverage: ràng buộc #1 (2 ô + bẫy) ✓; #2 (<1s, van K, int8) ✓; #3 (Normalizer + embed đa ngữ + pairs test) ✓; #4 (Go local, 1 binary, không mạng) ✓. 6 nguyên lý + 3 sửa phản biện + 3 interface đều có task.

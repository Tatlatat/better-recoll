package engine

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"sfs/internal/chunk"
	"sfs/internal/dedupe"
	"sfs/internal/index"
	"sfs/internal/model"
	"sfs/internal/normalize"
	"sfs/internal/reader"
	"sfs/internal/search"
	"sfs/internal/store"
)

// Config represents the configuration for the Engine.
type Config struct {
	ModelRoot string
	IndexPath string
	RerankK   int
	DiffEmbed bool
}

// DefaultConfig creates a Config with specified root paths.
func DefaultConfig(modelRoot, indexPath string) Config {
	if modelRoot == "" {
		modelRoot = model.DefaultModelRoot()
	} else {
		modelsPath := filepath.Join(modelRoot, "models")
		if info, err := os.Stat(modelsPath); err != nil || !info.IsDir() {
			modelRoot = model.DefaultModelRoot()
		}
	}
	return Config{
		ModelRoot: modelRoot,
		IndexPath: indexPath,
	}
}

// Result holds the details of a single search hit.
type Result struct {
	FilePath string
	Text     string
	Score    float32
	ChunkID  int64
}

// Engine wraps the embedder, chunk storage, and text/vector search indexes.
// SafeEmbedder wraps model.OnnxEmbedder to serialize calls to Embed.
type SafeEmbedder struct {
	*model.OnnxEmbedder
	mu sync.Mutex
}

// Embed calls model.OnnxEmbedder.Embed thread-safely.
func (s *SafeEmbedder) Embed(texts []string) ([][]float32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.OnnxEmbedder.Embed(texts)
}

type Engine struct {
	mu            sync.Mutex
	embedder      *SafeEmbedder       // search embedder (accessed by rerank.go)
	reranker      *model.OnnxReranker // search reranker (accessed by rerank.go)
	indexEmbedder *SafeEmbedder       // indexing embedder
	indexReranker *model.OnnxReranker // indexing reranker
	store         *store.FileStore
	bm25          *index.BM25
	vindex        *index.VectorIndex
	nextID        int64
	rerankK       int
	Router        *search.Router
	diffEmbed     bool
}

// New instantiates a new search engine.
// indexThreadCap giới hạn số ONNX intra-op thread cho INDEX nền: ~1/4 số core,
// tối thiểu 2. Để index không làm nóng máy / chặn tác vụ khác. Search KHÔNG bị
// giới hạn (dùng hết core cho nhanh). Có thể override bằng env SFS_INDEX_THREADS.
func indexThreadCap() int {
	if v := os.Getenv("SFS_INDEX_THREADS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	cap := runtime.NumCPU() / 4
	if cap < 2 {
		cap = 2
	}
	return cap
}

func New(cfg Config) (*Engine, error) {
	// Setup Onnx Config
	onnxCfg := model.DefaultOnnxConfig()
	onnxCfg.ModelPath = filepath.Join(cfg.ModelRoot, onnxCfg.ModelPath)
	onnxCfg.TokenizerPath = filepath.Join(cfg.ModelRoot, onnxCfg.TokenizerPath)

	// Create search embedder — KHÔNG giới hạn thread (nhanh nhất, latency <1s).
	embedder, err := model.NewOnnxEmbedder(onnxCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize embedder: %w", err)
	}

	// Create a SEPARATE index embedder. This is deliberate, not waste: indexing
	// runs many embed workers in parallel (see indexThrottled) AND search must
	// work while a background index is running. A single shared embedder would
	// serialize all index workers behind one mutex (turning parallel indexing
	// sequential — a 450KB file then takes minutes) and block search during
	// indexing. The duplicate model load costs ~1-2s at startup; that is the
	// price of concurrent index+search, which is a core feature.
	//
	// Index embedder bị GIỚI HẠN thread (≈1/4 core, tối thiểu 2): index nền không
	// được ngốn cả máy (đo được: không giới hạn → ONNX dùng ~11/16 core dù Go
	// Workers=1). Search embedder ở trên KHÔNG giới hạn nên vẫn nhanh.
	idxCfg := onnxCfg
	idxCfg.IntraOpThreads = indexThreadCap()
	indexEmbedder, err := model.NewOnnxEmbedder(idxCfg)
	if err != nil {
		embedder.Close()
		return nil, fmt.Errorf("failed to initialize index embedder: %w", err)
	}

	st, err := store.NewFileStore(cfg.IndexPath)
	if err != nil {
		indexEmbedder.Close()
		embedder.Close()
		return nil, fmt.Errorf("failed to initialize store: %w", err)
	}

	rerankerDir := filepath.Join(cfg.ModelRoot, "models/onnx/bge-reranker")
	rerankerTokenizerPath := rerankerDir
	// Prefer the int8-quantized reranker when present: it is ~4x faster per
	// candidate on CPU (the reranker is ~95% of search latency) with negligible
	// recall loss. Falls back to the full FP32 model if int8 isn't downloaded.
	rerankerPath := filepath.Join(rerankerDir, "model.onnx")
	int8Path := filepath.Join(rerankerDir, "model_int8.onnx")
	if _, err := os.Stat(int8Path); err == nil {
		rerankerPath = int8Path
		log.Printf("reranker: dùng bản int8 (nhanh ~4x): %s", int8Path)
	}

	// Create search reranker
	reranker, err := model.NewOnnxReranker(rerankerPath, rerankerTokenizerPath)
	if err != nil {
		st.Close()
		indexEmbedder.Close()
		embedder.Close()
		return nil, fmt.Errorf("failed to initialize reranker: %w", err)
	}

	// Separate index reranker (same rationale as the index embedder above).
	indexReranker, err := model.NewOnnxReranker(rerankerPath, rerankerTokenizerPath)
	if err != nil {
		reranker.Close()
		st.Close()
		indexEmbedder.Close()
		embedder.Close()
		return nil, fmt.Errorf("failed to initialize index reranker: %w", err)
	}

	bm25 := index.NewBM25()
	vindex := index.NewVectorIndex(embedder.Dim())

	// Load existing chunks if store contains any
	var maxID int64 = 0
	vectors, ids := st.AllVectors()
	for i, id := range ids {
		if id > maxID {
			maxID = id
		}
		ch, err := st.GetChunk(id)
		if err != nil {
			indexReranker.Close()
			reranker.Close()
			st.Close()
			indexEmbedder.Close()
			embedder.Close()
			return nil, fmt.Errorf("failed to retrieve existing chunk %d: %w", id, err)
		}
		bm25.Add(ch.ID, ch.NormText)
		if !(cfg.DiffEmbed && ch.IsBoilerplate) {
			vindex.Add(ch.ID, vectors[i])
		}
	}
	if len(ids) > 0 {
		bm25.Build()
	}

	// Van reranker: số candidate đưa vào cross-encoder = đánh đổi <1s vs recall.
	// Mặc định 5 đạt <1s vững trên CPU M-series (đo: avg 889ms, p-max 986ms);
	// máy mạnh tăng (RerankK=12+), máy yếu giảm. 2 lớp BM25+vector đã lọc trước
	// nên top-5 candidate gần như luôn chứa đáp án đúng — recall giữ cao.
	rerankK := cfg.RerankK
	if rerankK == 0 {
		rerankK = 5
	}

	e := &Engine{
		embedder:      &SafeEmbedder{OnnxEmbedder: embedder},
		reranker:      reranker,
		indexEmbedder: &SafeEmbedder{OnnxEmbedder: indexEmbedder},
		indexReranker: indexReranker,
		store:         st,
		bm25:          bm25,
		vindex:        vindex,
		nextID:        maxID + 1,
		rerankK:       rerankK,
		Router:        search.NewRouter(),
		diffEmbed:     cfg.DiffEmbed,
	}

	// Self-heal: an index polluted before the dedup/junk guards existed gets
	// cleaned automatically on load (drops junk-dir + duplicate chunks once).
	if removed, err := e.CompactJunk(); err != nil {
		log.Printf("engine: cảnh báo — dọn index rác lỗi: %v", err)
	} else if removed > 0 {
		log.Printf("engine: đã dọn %d đoạn rác/trùng khỏi chỉ mục (tự chữa)", removed)
	}

	return e, nil
}

type pendingChunk struct {
	filePath string
	text     string
	normText string
	offset   int
	modTime  int64
}

// OfficeExtensions là NGUỒN SỰ THẬT cho "định dạng dân văn phòng thật sự dùng".
//
// First Principles: đối tượng = dân văn phòng bận rộn. Họ tạo tài liệu bằng
// Word/Excel/PowerPoint/PDF — KHÔNG bao giờ tạo .md (đó là công cụ lập trình
// viên). Đo trên máy thật: 29k file .md (toàn README/docs của code clone) vs ~750
// file văn phòng thật. Lọc rác chính xác KHÔNG bằng cách blacklist thư mục (chặn
// không xuể, dễ nhầm) mà bằng cách CHỈ NHẬN ĐÚNG ĐỊNH DẠNG VĂN PHÒNG VÀO — rác
// lập trình tự loại vì .md/.go/.js không phải định dạng văn phòng.
//
// Chỉ liệt kê định dạng reader ĐỌC ĐƯỢC hiện tại. .pptx/.doc/.key cần reader
// riêng (TODO) — khi có sẽ thêm vào đây.
func OfficeExtensions() []string {
	return []string{
		"pdf",  // PDFReader
		"docx", // DocxReader (Word)
		"xlsx", // XLSXReader (Excel)
		"pptx", // PptxReader (PowerPoint)
		"rtf",  // RtfReader (Rich Text)
		"csv",  // TxtReader (bảng tính xuất CSV)
		// TODO khi có reader: "doc","ppt","key","pages","numbers","odt"
	}
}

// IndexOptions represents parameters for throttled indexing.
type IndexOptions struct {
	Workers             int
	BatchSize           int
	PauseBetweenBatches time.Duration
	OnlyExtensions      []string
	MaxFileBytes        int64
}

// FastIndexOptions returns options optimized for fast onboarding. BatchSize 32
// roughly halves per-chunk embed cost vs the old 8 (measured ~870ms→~507ms/chunk)
// because the ONNX forward pass amortizes over a larger batch.
func FastIndexOptions() IndexOptions {
	return IndexOptions{
		Workers:             runtime.NumCPU(),
		BatchSize:           32,
		PauseBetweenBatches: 0,
	}
}

// BackgroundIndexOptions returns options optimized for cool background indexing.
// Mặc định CHỈ index định dạng văn phòng (OfficeExtensions) — đây là tuyến lọc
// rác chính: 29k .md/code rác tự loại vì không phải định dạng văn phòng. Người
// gọi muốn index loại khác (vd code) phải tự đặt OnlyExtensions khác.
func BackgroundIndexOptions() IndexOptions {
	return IndexOptions{
		Workers:             1,
		BatchSize:           4,
		PauseBetweenBatches: 600 * time.Millisecond,
		OnlyExtensions:      OfficeExtensions(),
	}
}

// Index walks the directory recursively, parses supported files,
// chunks their text, embeds chunks, and writes/indexes them.
func (e *Engine) Index(dir string) error {
	return e.IndexThrottled(dir, FastIndexOptions())
}

// IndexThrottled walks the directory recursively, collects supported files,
// and indexes them in throttled batches to prevent CPU overheating.
func (e *Engine) IndexThrottled(dir string, opts IndexOptions) error {
	return e.indexThrottled(dir, opts)
}

// isJunkDir reports whether a directory name is build/cache/dependency clutter
// that must never be indexed (mirrors the webui background-walker skip list).
// Catches the "LoremIpsum.txt 6270 chunks" class of junk under build/checkouts.
func isJunkDir(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "library", "system", "node_modules", "caches", "cache",
		"build", "deriveddata", "checkouts", "sourcepackages",
		"pods", "vendor", "dist", "target", "__pycache__",
		".git", ".svn", "venv", ".venv", "lora_datasets",
		"site-packages", "dist-packages", "node-gyp", ".tox",
		".mypy_cache", ".pytest_cache", ".gradle", ".cargo",
		".rustup", ".npm", ".cache":
		return true
	}
	// Python egg/dist metadata dirs (e.g. foo-1.2.egg-info).
	if strings.HasSuffix(lower, ".egg-info") || strings.HasSuffix(lower, ".dist-info") {
		return true
	}
	return false
}

// pathHasJunkSegment reports whether ANY segment of the path is a junk dir.
// Catches the case where the index TARGET itself is already inside a junk
// tree (e.g. dirs.json legacy entries pointing at build/SourcePackages/...).
func pathHasJunkSegment(p string) bool {
	for _, seg := range strings.Split(filepath.Clean(p), string(filepath.Separator)) {
		if seg != "" && isJunkDir(seg) {
			return true
		}
	}
	return false
}

func (e *Engine) indexThrottled(dir string, opts IndexOptions) error {
	// Refuse to index a target that is itself inside a junk tree. Legacy
	// dirs.json entries pointed straight at build/SourcePackages/checkouts/...
	// so the per-file junk skip never fired (the walk root was already junk).
	if pathHasJunkSegment(dir) {
		log.Printf("index: bỏ qua thư mục rác (nằm trong build/checkouts/...): %s", dir)
		return nil
	}


	var pending []pendingChunk
	skipped := 0
	alreadyIndexed := 0
	finder := dedupe.New(2)

	// Files already in the store: skip them so re-indexing an overlapping
	// directory does NOT create duplicate chunks. This is the root-cause fix
	// for the "index nhân bản" bug (a file getting indexed 6x → pool đầy bản
	// sao → file đúng không lọt → vừa chậm vừa sai).
	existing := e.store.IndexedFilePaths()

	cleanDir := filepath.Clean(dir)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if filepath.Clean(path) != cleanDir &&
				(strings.HasPrefix(info.Name(), ".") || isJunkDir(info.Name())) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(info.Name(), ".") {
			return nil
		}

		if existing[path] {
			alreadyIndexed++
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if _, ok := reader.Registry[ext]; !ok {
			return nil
		}

		if len(opts.OnlyExtensions) > 0 {
			allowed := false
			for _, allowedExt := range opts.OnlyExtensions {
				a := strings.ToLower(allowedExt)
				if !strings.HasPrefix(a, ".") {
					a = "." + a
				}
				if ext == a {
					allowed = true
					break
				}
			}
			if !allowed {
				return nil
			}
		}

		if opts.MaxFileBytes > 0 && info.Size() > opts.MaxFileBytes {
			return nil
		}

		text, err := reader.ReadFile(path)
		if err != nil {
			// Một file xấu KHÔNG được làm sập cả index — bỏ qua, ghi log, tiếp tục.
			// Thư mục thật luôn có file lỗi (PDF hỏng, mã hoá lạ, v.v.).
			log.Printf("bỏ qua (đọc lỗi) %s: %v", path, err)
			skipped++
			return nil
		}

		mt := info.ModTime().Unix()

		chunks := chunk.Chunk(text, 512)
		for _, ch := range chunks {
			finder.Add(ch.Text)
			pending = append(pending, pendingChunk{
				filePath: path,
				text:     ch.Text,
				normText: normalize.Normalize(ch.Text),
				offset:   ch.Offset,
				modTime:  mt,
			})
		}
		return nil
	})
	if err != nil {
		return err
	}

	if skipped > 0 {
		log.Printf("index: bỏ qua %d file đọc lỗi, tiếp tục với %d đoạn", skipped, len(pending))
	}
	if alreadyIndexed > 0 {
		log.Printf("index: bỏ qua %d file đã có trong chỉ mục (tránh nhân bản)", alreadyIndexed)
	}

	if len(pending) == 0 {
		// Record the indexed directory even if empty
		e.mu.Lock()
		err := e.store.AddIndexedDir(dir)
		e.mu.Unlock()
		return err
	}

	// Finalize dedupe finder (first pass completed)
	finder.Build()

	// Embedding throughput improves markedly with batch size: measured per-chunk
	// cost on this CPU model is ~870ms at batch 8 but ~423ms at batch 64 (≈2x
	// faster) because the ONNX forward pass amortizes over the batch. We therefore
	// honor the caller's BatchSize (capped at 64 to bound memory/latency) instead
	// of the old hard cap of 8, which threw that speedup away. Background indexing
	// still asks for a small batch (4) to stay cool; fast onboarding asks for more.
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = 32
	}
	if batchSize > 64 {
		batchSize = 64
	}
	workers := opts.Workers
	if workers <= 0 {
		workers = 1
	}

	var batches [][]pendingChunk
	for i := 0; i < len(pending); i += batchSize {
		end := i + batchSize
		if end > len(pending) {
			end = len(pending)
		}
		batches = append(batches, pending[i:end])
	}

	type batchJob struct {
		chunk []pendingChunk
	}

	jobs := make(chan batchJob, len(batches))
	for _, b := range batches {
		jobs <- batchJob{chunk: b}
	}
	close(jobs)

	var wg sync.WaitGroup
	var errOnce sync.Once
	var workerErr error

	setWorkerErr := func(err error) {
		errOnce.Do(func() {
			workerErr = err
		})
	}

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				e.mu.Lock()
				hasErr := workerErr != nil
				e.mu.Unlock()
				if hasErr {
					return
				}

				texts := make([]string, len(job.chunk))
				for i, pc := range job.chunk {
					texts[i] = pc.text
				}

				vectors, err := e.indexEmbedder.Embed(texts)
				if err != nil {
					setWorkerErr(fmt.Errorf("failed to embed chunk texts: %w", err))
					return
				}

				e.mu.Lock()
				if workerErr != nil {
					e.mu.Unlock()
					return
				}

				storeChunks := make([]store.Chunk, len(job.chunk))
				for i, pc := range job.chunk {
					id := e.nextID
					e.nextID++

					storeChunks[i] = store.Chunk{
						ID:            id,
						FilePath:      pc.filePath,
						Text:          pc.text,
						NormText:      pc.normText,
						Offset:        pc.offset,
						Vector:        vectors[i],
						IsBoilerplate: finder.IsBoilerplate(pc.text),
						ModTime:       pc.modTime,
					}
				}

				if err := e.store.Write(storeChunks); err != nil {
					setWorkerErr(fmt.Errorf("failed to write chunks to store: %w", err))
					e.mu.Unlock()
					return
				}

				for _, sc := range storeChunks {
					e.bm25.Add(sc.ID, sc.NormText)
					if !(e.diffEmbed && sc.IsBoilerplate) {
						e.vindex.Add(sc.ID, sc.Vector)
					}
				}
				e.mu.Unlock()

				if opts.PauseBetweenBatches > 0 {
					time.Sleep(opts.PauseBetweenBatches)
				}
			}
		}()
	}
	wg.Wait()

	if workerErr != nil {
		return workerErr
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Finalize BM25 IDF parameters
	e.bm25.Build()

	// Record the indexed directory
	if err := e.store.AddIndexedDir(dir); err != nil {
		return err
	}

	return nil
}

// Search queries the vector and BM25 index, merges/dedupes findings, and returns sorted top-k results.
func (e *Engine) Search(query string, k int) ([]Result, error) {
	if k <= 0 {
		return nil, nil
	}

	// Embed query
	queryEmbs, err := e.embedder.Embed([]string{query})
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}
	queryVector := queryEmbs[0]

	e.mu.Lock()
	defer e.mu.Unlock()

	// Get Top-K from vector index and BM25 index
	vectorResults := e.vindex.Search(queryVector, k)
	bm25Results := e.bm25.Search(normalize.Normalize(query), k)

	// Merge candidate chunk IDs (union) and dedupe
	// "use the vector score; if only in BM25 use its score"
	mergedScores := make(map[int64]float32)
	for _, r := range bm25Results {
		mergedScores[r.ID] = r.Score
	}
	for _, r := range vectorResults {
		mergedScores[r.ID] = r.Score
	}

	// Load chunks and construct Result objects
	results := make([]Result, 0, len(mergedScores))
	for id, score := range mergedScores {
		ch, err := e.store.GetChunk(id)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve chunk %d: %w", id, err)
		}
		results = append(results, Result{
			FilePath: ch.FilePath,
			Text:     ch.Text,
			Score:    score,
			ChunkID:  id,
		})
	}

	// Sort results desc by score. If scores are equal, sort by ChunkID.
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].ChunkID < results[j].ChunkID
		}
		return results[i].Score > results[j].Score
	})

	// Slice to top-k
	if len(results) > k {
		results = results[:k]
	}

	return results, nil
}

// Close releases the resources held by the embedder and store.
// ChunkCount returns the number of chunks currently stored.
func (e *Engine) ChunkCount() int {
	return e.store.Count()
}

func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var errs []error
	if e.embedder != nil {
		if err := e.embedder.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if e.reranker != nil {
		if err := e.reranker.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	// indexEmbedder/indexReranker are SEPARATE instances from the search ones
	// (see New), so each must be closed exactly once.
	if e.indexEmbedder != nil {
		if err := e.indexEmbedder.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if e.indexReranker != nil {
		if err := e.indexReranker.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if e.store != nil {
		if err := e.store.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// CompactJunk removes chunks under junk dirs (build/checkouts/node_modules/...)
// and collapses exact-duplicate chunks, then rebuilds the in-memory BM25 and
// vector indexes from the surviving chunks. Permanent cleanup for indexes that
// were polluted before the indexer-side guards existed. Returns chunks removed.
func (e *Engine) CompactJunk() (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	before := e.store.Count()
	kept, err := e.store.Compact(
		func(c store.Chunk) bool { return !pathHasJunkSegment(c.FilePath) },
		func(d string) bool { return !pathHasJunkSegment(d) },
	)
	if err != nil {
		return 0, err
	}

	// Rebuild in-memory indexes from surviving chunks.
	e.bm25 = index.NewBM25()
	e.vindex = index.NewVectorIndex(e.embedder.Dim())
	var maxID int64
	for _, c := range kept {
		if c.ID > maxID {
			maxID = c.ID
		}
		e.bm25.Add(c.ID, c.NormText)
		if !(e.diffEmbed && c.IsBoilerplate) {
			e.vindex.Add(c.ID, c.Vector)
		}
	}
	if len(kept) > 0 {
		e.bm25.Build()
	}
	e.nextID = maxID + 1

	return before - len(kept), nil
}

// Reset clears the in-memory BM25, Vector index, and the store (deleting all chunks),
// resetting nextID to 1.
func (e *Engine) Reset() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.reset()
}

func (e *Engine) reset() error {
	if err := e.store.Clear(); err != nil {
		return fmt.Errorf("failed to clear store: %w", err)
	}

	e.bm25 = index.NewBM25()
	e.vindex = index.NewVectorIndex(e.embedder.Dim())
	e.nextID = 1

	return nil
}

// ReindexAll clears all indexes and re-indexes the specified directories cleanly.
func (e *Engine) ReindexAll(dirs []string, opts IndexOptions) error {
	e.mu.Lock()
	err := e.reset()
	e.mu.Unlock()
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		if err := e.indexThrottled(dir, opts); err != nil {
			return err
		}
	}
	return nil
}

// IndexedDirs returns the list of indexed directories.
func (e *Engine) IndexedDirs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.store.IndexedDirs()
}

// FileEntries trả danh sách file đã index (cho predictor).
func (e *Engine) FileEntries() []store.FileEntry {
	return e.store.FileEntries()
}

// EmbedQuery embed 1 query thành vector (cho interest vector). Lỗi → nil.
func (e *Engine) EmbedQuery(q string) []float32 {
	vecs, err := e.embedder.Embed([]string{q})
	if err != nil || len(vecs) == 0 {
		return nil
	}
	return vecs[0]
}

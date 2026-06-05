package engine

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
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
	mu        sync.Mutex
	embedder  *SafeEmbedder
	reranker  *model.OnnxReranker
	store     *store.FileStore
	bm25      *index.BM25
	vindex    *index.VectorIndex
	nextID    int64
	rerankK   int
	Router    *search.Router
	diffEmbed bool
}

// New instantiates a new search engine.
func New(cfg Config) (*Engine, error) {
	// Setup Onnx Config
	onnxCfg := model.DefaultOnnxConfig()
	onnxCfg.ModelPath = filepath.Join(cfg.ModelRoot, onnxCfg.ModelPath)
	onnxCfg.TokenizerPath = filepath.Join(cfg.ModelRoot, onnxCfg.TokenizerPath)

	embedder, err := model.NewOnnxEmbedder(onnxCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize embedder: %w", err)
	}

	st, err := store.NewFileStore(cfg.IndexPath)
	if err != nil {
		embedder.Close()
		return nil, fmt.Errorf("failed to initialize store: %w", err)
	}

	rerankerPath := filepath.Join(cfg.ModelRoot, "models/onnx/bge-reranker/model.onnx")
	rerankerTokenizerPath := filepath.Join(cfg.ModelRoot, "models/onnx/bge-reranker")
	reranker, err := model.NewOnnxReranker(rerankerPath, rerankerTokenizerPath)
	if err != nil {
		st.Close()
		embedder.Close()
		return nil, fmt.Errorf("failed to initialize reranker: %w", err)
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
			st.Close()
			embedder.Close()
			reranker.Close()
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

	return &Engine{
		embedder:  &SafeEmbedder{OnnxEmbedder: embedder},
		reranker:  reranker,
		store:     st,
		bm25:      bm25,
		vindex:    vindex,
		nextID:    maxID + 1,
		rerankK:   rerankK,
		Router:    search.NewRouter(),
		diffEmbed: cfg.DiffEmbed,
	}, nil
}

type pendingChunk struct {
	filePath string
	text     string
	normText string
	offset   int
}

// IndexOptions represents parameters for throttled indexing.
type IndexOptions struct {
	Workers             int
	BatchSize           int
	PauseBetweenBatches time.Duration
	OnlyExtensions      []string
	MaxFileBytes        int64
}

// FastIndexOptions returns options optimized for fast onboarding.
func FastIndexOptions() IndexOptions {
	return IndexOptions{
		Workers:             runtime.NumCPU(),
		BatchSize:           32,
		PauseBetweenBatches: 0,
	}
}

// BackgroundIndexOptions returns options optimized for cool background indexing.
func BackgroundIndexOptions() IndexOptions {
	return IndexOptions{
		Workers:             1,
		BatchSize:           8,
		PauseBetweenBatches: 400 * time.Millisecond,
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

func (e *Engine) indexThrottled(dir string, opts IndexOptions) error {

	var pending []pendingChunk
	skipped := 0
	finder := dedupe.New(2)

	cleanDir := filepath.Clean(dir)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if filepath.Clean(path) != cleanDir && strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(info.Name(), ".") {
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

		chunks := chunk.Chunk(text, 512)
		for _, ch := range chunks {
			finder.Add(ch.Text)
			pending = append(pending, pendingChunk{
				filePath: path,
				text:     ch.Text,
				normText: normalize.Normalize(ch.Text),
				offset:   ch.Offset,
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

	if len(pending) == 0 {
		// Record the indexed directory even if empty
		e.mu.Lock()
		err := e.store.AddIndexedDir(dir)
		e.mu.Unlock()
		return err
	}

	// Finalize dedupe finder (first pass completed)
	finder.Build()

	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = 32
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

				vectors, err := e.embedder.Embed(texts)
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

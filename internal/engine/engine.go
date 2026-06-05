package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

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
type Engine struct {
	mu        sync.Mutex
	embedder  *model.OnnxEmbedder
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
		embedder:  embedder,
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

// Index walks the directory recursively, parses supported files,
// chunks their text, embeds chunks, and writes/indexes them.
func (e *Engine) Index(dir string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var pending []pendingChunk
	finder := dedupe.New(2)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if _, ok := reader.Registry[ext]; !ok {
			return nil
		}

		text, err := reader.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", path, err)
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

	if len(pending) == 0 {
		return nil
	}

	// Finalize dedupe finder (first pass completed)
	finder.Build()

	// Batch-embed all chunk texts
	textsToEmbed := make([]string, len(pending))
	for i, pc := range pending {
		textsToEmbed[i] = pc.text
	}
	vectors, err := e.embedder.Embed(textsToEmbed)
	if err != nil {
		return fmt.Errorf("failed to embed chunk texts: %w", err)
	}

	// Build store.Chunk records (second pass: set IsBoilerplate)
	storeChunks := make([]store.Chunk, len(pending))
	for i, pc := range pending {
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

	// Write chunks to store
	if err := e.store.Write(storeChunks); err != nil {
		return fmt.Errorf("failed to write chunks to store: %w", err)
	}

	// Add to indexes (respect DiffEmbed)
	for _, sc := range storeChunks {
		e.bm25.Add(sc.ID, sc.NormText)
		if !(e.diffEmbed && sc.IsBoilerplate) {
			e.vindex.Add(sc.ID, sc.Vector)
		}
	}

	// Finalize BM25 IDF parameters
	e.bm25.Build()

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

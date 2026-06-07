package webui

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"sfs/internal/engine"
	"sfs/internal/intent"
	"sfs/internal/store"
)

//go:embed assets/*
var assetsFS embed.FS

// Global State
var (
	engineMutex  sync.RWMutex
	globalEngine *engine.Engine
	globalConfig engine.Config

	setupMutex      sync.Mutex
	isDownloading   bool
	downloadPercent int
	downloadStatus  string
	downloadErr     error

	indexedFoldersMutex sync.Mutex
	indexedFolders      []string

	indexingMutex         sync.Mutex
	isIndexing            bool
	indexDir              string
	indexMode             string
	initialCount          int
	currentCount          int
	isHomeIndexingRunning bool

	behaviorLog *intent.Log
)


func handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	engineMutex.RLock()
	eng := globalEngine
	engineMutex.RUnlock()

	if eng == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Mô hình AI chưa được tải. Hãy tải mô hình trước.",
		})
		return
	}

	type SearchResultItem struct {
		FilePath string  `json:"filePath"`
		Text     string  `json:"text"`
		Score    float32 `json:"score"`
	}

	type SearchResponse struct {
		Exact   []SearchResultItem `json:"exact"`
		Suggest []SearchResultItem `json:"suggest"`
		Stage   string             `json:"stage"` // "fast" (thô, ~40ms) hoặc "final" (đã rerank)
	}

	q := r.URL.Query().Get("q")
	if q == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SearchResponse{
			Exact:   []SearchResultItem{},
			Suggest: []SearchResultItem{},
			Stage:   "final",
		})
		return
	}

	// Stage-1 (fast=1): kết quả thô ~40ms để hiện NGAY khi đang gõ.
	// Mặc định: kết quả tinh (rerank) <1s.
	fast := r.URL.Query().Get("fast") == "1"
	stage := "final"
	var results engine.RankedResults
	var err error
	if fast {
		stage = "fast"
		results, err = eng.SearchFast(q, 10)
	} else {
		results, err = eng.SearchRanked(q, 10)
	}
	if err != nil {
		log.Printf("Search error: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("Tìm kiếm thất bại: %v", err),
		})
		return
	}

	exact := make([]SearchResultItem, 0, len(results.Exact))
	for _, item := range results.Exact {
		exact = append(exact, SearchResultItem{
			FilePath: item.FilePath,
			Text:     item.Text,
			Score:    item.Score,
		})
	}

	suggest := make([]SearchResultItem, 0, len(results.Suggest))
	for _, item := range results.Suggest {
		suggest = append(suggest, SearchResultItem{
			FilePath: item.FilePath,
			Text:     item.Text,
			Score:    item.Score,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(SearchResponse{
		Exact:   exact,
		Suggest: suggest,
		Stage:   stage,
	}); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}

func getStoreCount(indexPath string) int {
	gobPath := filepath.Join(indexPath, "chunks.gob")
	if _, err := os.Stat(gobPath); os.IsNotExist(err) {
		return 0
	}
	st, err := store.NewFileStore(indexPath)
	if err != nil {
		return 0
	}
	defer st.Close()
	return st.Count()
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	engineMutex.RLock()
	eng := globalEngine
	cfg := globalConfig
	engineMutex.RUnlock()

	if eng == nil {
		http.Error(w, "Mô hình AI chưa được tải. Hãy tải mô hình trước.", http.StatusServiceUnavailable)
		return
	}

	type IndexRequest struct {
		Dir  string `json:"dir"`
		Mode string `json:"mode"`
		// OnlyExtensions: nếu có, CHỈ index các đuôi này (vd ["md","txt","pdf",
		// "docx"] = chỉ tài liệu, bỏ code framework). Rỗng = mọi đuôi reader hỗ trợ.
		OnlyExtensions []string `json:"onlyExtensions"`
	}

	var req IndexRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.Dir == "" {
		http.Error(w, "Directory path 'dir' is required", http.StatusBadRequest)
		return
	}

	indexingMutex.Lock()
	if isIndexing {
		indexingMutex.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"busy": true})
		return
	}
	isIndexing = true
	indexDir = req.Dir
	indexMode = req.Mode
	if indexMode == "" {
		indexMode = "background"
	}
	indexingMutex.Unlock()

	onlyExt := req.OnlyExtensions
	go func() {
		var opts engine.IndexOptions
		if indexMode == "fast" {
			opts = engine.FastIndexOptions()
		} else {
			opts = engine.BackgroundIndexOptions()
		}
		// Giới hạn đuôi file nếu request yêu cầu (vd chỉ tài liệu, bỏ code).
		if len(onlyExt) > 0 {
			opts.OnlyExtensions = onlyExt
		}

		startCount := getStoreCount(cfg.IndexPath)
		indexingMutex.Lock()
		initialCount = startCount
		currentCount = startCount
		indexingMutex.Unlock()

		log.Printf("Starting background index of %s in %s mode", req.Dir, indexMode)
		err := eng.IndexThrottled(req.Dir, opts)

		endCount := getStoreCount(cfg.IndexPath)

		indexingMutex.Lock()
		isIndexing = false
		currentCount = endCount
		if err != nil {
			log.Printf("Background indexing error: %v", err)
		} else {
			log.Printf("Background indexing finished successfully. Chunks: %d -> %d", startCount, endCount)

			// Add to memory list of folders
			indexedFoldersMutex.Lock()
			exists := false
			for _, f := range indexedFolders {
				if f == req.Dir {
					exists = true
					break
				}
			}
			if !exists {
				indexedFolders = append(indexedFolders, req.Dir)
			}
			indexedFoldersMutex.Unlock()
		}
		indexingMutex.Unlock()
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"started": true})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type HealthResponse struct {
		Ok bool `json:"ok"`
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(HealthResponse{Ok: true})
}

func handleFolders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	indexedFoldersMutex.Lock()
	folders := indexedFolders
	if folders == nil {
		folders = []string{}
	}
	indexedFoldersMutex.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(folders)
}

type StatusResponse struct {
	Missing      bool   `json:"missing"`
	Downloading  bool   `json:"downloading"`
	Percent      int    `json:"percent"`
	Status       string `json:"status"`
	Error        string `json:"error"`
	Onboarded    bool   `json:"onboarded"`
	Indexing     bool   `json:"indexing"`
	CurrentDir   string `json:"currentDir"`
	FilesIndexed int    `json:"filesIndexed"`
	Phase        string `json:"phase"`
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	engineMutex.RLock()
	hasEngine := globalEngine != nil
	cfg := globalConfig
	engineMutex.RUnlock()

	setupMutex.Lock()
	resp := StatusResponse{
		Missing:     !hasEngine,
		Downloading: isDownloading,
		Percent:     downloadPercent,
		Status:      downloadStatus,
	}
	if downloadErr != nil {
		resp.Error = downloadErr.Error()
	}
	setupMutex.Unlock()

	stateMutex.Lock()
	onboarded := globalState.Onboarded
	stateMutex.Unlock()
	resp.Onboarded = onboarded

	indexingMutex.Lock()
	resp.Indexing = isIndexing || isHomeIndexingRunning
	resp.CurrentDir = indexDir
	if isIndexing {
		resp.Phase = indexMode
	} else if isHomeIndexingRunning {
		resp.Phase = "background"
	} else {
		resp.Phase = "idle"
	}
	initC := initialCount
	indexingMutex.Unlock()

	if isIndexing {
		currC := getStoreCount(cfg.IndexPath)
		diff := currC - initC
		if diff < 0 {
			diff = 0
		}
		resp.FilesIndexed = diff
	} else {
		resp.FilesIndexed = 0
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	setupMutex.Lock()
	if isDownloading {
		setupMutex.Unlock()
		http.Error(w, "Already downloading", http.StatusConflict)
		return
	}
	isDownloading = true
	downloadPercent = 0
	downloadStatus = "Bắt đầu tải..."
	downloadErr = nil
	setupMutex.Unlock()

	go func() {
		engineMutex.RLock()
		cfg := globalConfig
		engineMutex.RUnlock()

		err := downloadAndSetupModels(cfg)
		setupMutex.Lock()
		isDownloading = false
		if err != nil {
			downloadErr = err
			downloadStatus = "Lỗi: " + err.Error()
		} else {
			downloadStatus = "Khởi tạo công cụ tìm kiếm..."
			eng, initErr := engine.New(cfg)
			if initErr != nil {
				downloadErr = initErr
				downloadStatus = "Lỗi khởi tạo: " + initErr.Error()
			} else {
				engineMutex.Lock()
				globalEngine = eng
				engineMutex.Unlock()
				downloadStatus = "Đã hoàn thành!"

				stateMutex.Lock()
				onboarded := globalState.Onboarded
				stateMutex.Unlock()
				if onboarded {
					// V2: chỉ refresh thư mục user đã chọn, KHÔNG nuốt home.
					go refreshIndexedDirs(eng)
				}
			}
		}
		setupMutex.Unlock()
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"started": true})
}

func handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, err := assetsFS.ReadFile("assets/index.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

type AppState struct {
	Onboarded  bool   `json:"onboarded"`
	PrimaryDir string `json:"primaryDir"`
}

var (
	stateMutex  sync.Mutex
	globalState AppState
)

func loadState(indexPath string) AppState {
	statePath := filepath.Join(filepath.Dir(indexPath), "state.json")
	file, err := os.Open(statePath)
	if err != nil {
		return AppState{}
	}
	defer file.Close()
	var state AppState
	if err := json.NewDecoder(file).Decode(&state); err != nil {
		log.Printf("Error decoding state file: %v", err)
		return AppState{}
	}
	return state
}

func saveState(indexPath string, state AppState) {
	statePath := filepath.Join(filepath.Dir(indexPath), "state.json")
	os.MkdirAll(filepath.Dir(statePath), 0755)
	file, err := os.Create(statePath)
	if err != nil {
		log.Printf("Error creating state file: %v", err)
		return
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(state); err != nil {
		log.Printf("Error encoding state file: %v", err)
	}
}

// findSafeTrees walk toàn home tìm thư mục "an toàn". CHỈ còn được dùng bởi
// runBackgroundHomeIndexing (đã NGỪNG gọi tự động — xem V2). Giữ lại phòng khi
// muốn tính năng "index cả home" như một lựa chọn user CHỦ ĐỘNG bật, không auto.
func findSafeTrees(dirPath string, primaryDir string) (bool, []string) {
	name := filepath.Base(dirPath)

	if primaryDir != "" && strings.EqualFold(filepath.Clean(dirPath), filepath.Clean(primaryDir)) {
		return false, nil
	}

	if strings.HasPrefix(name, ".") {
		return false, nil
	}

	lowerName := strings.ToLower(name)
	switch lowerName {
	case "library", "system", "node_modules", "caches", "cache",
		"build", "deriveddata", "checkouts", "sourcepackages",
		"pods", "vendor", "dist", "target", "__pycache__",
		"venv", ".venv", "lora_datasets", "site-packages",
		"dist-packages", ".tox", ".mypy_cache", ".pytest_cache",
		".gradle", ".cargo", ".rustup", ".npm":
		return false, nil
	}
	if strings.HasSuffix(lowerName, ".egg-info") || strings.HasSuffix(lowerName, ".dist-info") {
		return false, nil
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return true, nil
	}

	mySafe := true
	var allChildren []string
	var subTrees []string

	for _, entry := range entries {
		if entry.IsDir() {
			childPath := filepath.Join(dirPath, entry.Name())
			childSafe, childSafeTrees := findSafeTrees(childPath, primaryDir)
			if !childSafe {
				mySafe = false
				subTrees = append(subTrees, childSafeTrees...)
			} else {
				allChildren = append(allChildren, childPath)
			}
		}
	}

	if mySafe {
		return true, []string{dirPath}
	}

	merged := append(allChildren, subTrees...)
	return false, merged
}

// refreshIndexedDirs re-quét CHỈ các thư mục user ĐÃ chọn (đã có trong store), để
// nhặt file mới xuất hiện. KHÔNG quét toàn bộ home dir.
//
// Đây là sửa V2: trước đây runBackgroundHomeIndexing walk cả os.UserHomeDir()
// (~1891 thư mục) mỗi lần khởi động → "mở máy lên là CPU chạy, tự nhiên đang
// index". User chỉ CHỌN vài thư mục, app KHÔNG được tự suy ra "được phép nuốt cả
// home". Per-file dedup (existing[path]) lo việc bỏ file đã index, nên re-quét
// dir đã chọn là rẻ và đúng (chỉ file mới được embed).
func refreshIndexedDirs(eng *engine.Engine) {
	indexingMutex.Lock()
	if isHomeIndexingRunning {
		indexingMutex.Unlock()
		return
	}
	isHomeIndexingRunning = true
	indexingMutex.Unlock()
	defer func() {
		indexingMutex.Lock()
		isHomeIndexingRunning = false
		indexingMutex.Unlock()
	}()

	dirs := eng.IndexedDirs()
	if len(dirs) == 0 {
		log.Println("refresh: chưa có thư mục nào được chọn để index — bỏ qua (không tự nuốt home).")
		return
	}
	log.Printf("refresh: cập nhật %d thư mục user đã chọn (không quét toàn home).", len(dirs))
	for _, dir := range dirs {
		indexingMutex.Lock()
		for isIndexing {
			indexingMutex.Unlock()
			time.Sleep(2 * time.Second)
			indexingMutex.Lock()
		}
		isIndexing = true
		indexDir = dir
		indexMode = "background"
		cfg := globalConfig
		startCount := getStoreCount(cfg.IndexPath)
		initialCount = startCount
		currentCount = startCount
		indexingMutex.Unlock()

		opts := engine.BackgroundIndexOptions()
		if err := eng.IndexThrottled(dir, opts); err != nil {
			log.Printf("refresh: lỗi cập nhật %s: %v", dir, err)
		}

		indexingMutex.Lock()
		isIndexing = false
		indexingMutex.Unlock()
	}
	log.Println("refresh: xong cập nhật thư mục đã chọn.")
}

// runBackgroundHomeIndexing (CŨ — KHÔNG còn gọi tự động). Walk toàn bộ home dir.
// Giữ lại tham chiếu nhưng đã thay bằng refreshIndexedDirs ở startup (xem V2).
func runBackgroundHomeIndexing(eng *engine.Engine, primaryDir string) {
	indexingMutex.Lock()
	if isHomeIndexingRunning {
		indexingMutex.Unlock()
		log.Println("Background home indexing is already running, skipping start.")
		return
	}
	isHomeIndexingRunning = true
	indexingMutex.Unlock()

	defer func() {
		indexingMutex.Lock()
		isHomeIndexingRunning = false
		indexingMutex.Unlock()
	}()

	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("Error getting user home directory: %v", err)
		return
	}

	_, finalDirs := findSafeTrees(home, primaryDir)

	// Build a map of already indexed directories for fast checking.
	alreadyIndexed := make(map[string]bool)
	for _, d := range eng.IndexedDirs() {
		alreadyIndexed[filepath.Clean(d)] = true
	}

	// Filter finalDirs to only index directories not already indexed
	var dirsToIndex []string
	for _, dir := range finalDirs {
		if alreadyIndexed[filepath.Clean(dir)] {
			continue
		}
		dirsToIndex = append(dirsToIndex, dir)
	}

	log.Printf("Starting background home directory indexing. Found %d safe directories to index, %d are not yet indexed.", len(finalDirs), len(dirsToIndex))

	for _, dir := range dirsToIndex {
		indexingMutex.Lock()
		for isIndexing {
			indexingMutex.Unlock()
			time.Sleep(2 * time.Second)
			indexingMutex.Lock()
		}
		isIndexing = true
		indexDir = dir
		indexMode = "background"
		cfg := globalConfig
		startCount := getStoreCount(cfg.IndexPath)
		initialCount = startCount
		currentCount = startCount
		indexingMutex.Unlock()

		log.Printf("Background indexing home subdirectory: %s", dir)
		opts := engine.BackgroundIndexOptions()
		err := eng.IndexThrottled(dir, opts)
		if err != nil {
			log.Printf("Error indexing subdirectory %s: %v", dir, err)
		}

		indexingMutex.Lock()
		isIndexing = false
		indexingMutex.Unlock()
	}

	log.Printf("Finished background home directory indexing.")
}

func handleOnboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	engineMutex.RLock()
	eng := globalEngine
	cfg := globalConfig
	engineMutex.RUnlock()

	if eng == nil {
		http.Error(w, "Mô hình AI chưa được tải. Hãy tải mô hình trước.", http.StatusServiceUnavailable)
		return
	}

	type OnboardRequest struct {
		Dir string `json:"dir"`
	}

	var req OnboardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.Dir == "" {
		http.Error(w, "Directory path 'dir' is required", http.StatusBadRequest)
		return
	}

	indexingMutex.Lock()
	if isIndexing {
		indexingMutex.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"busy": true})
		return
	}
	isIndexing = true
	indexDir = req.Dir
	indexMode = "fast"
	indexingMutex.Unlock()

	stateMutex.Lock()
	globalState.Onboarded = true
	globalState.PrimaryDir = req.Dir
	saveState(cfg.IndexPath, globalState)
	stateMutex.Unlock()

	go func() {
		startCount := getStoreCount(cfg.IndexPath)
		indexingMutex.Lock()
		initialCount = startCount
		currentCount = startCount
		indexingMutex.Unlock()

		log.Printf("Onboarding: Starting Fast Index of %s", req.Dir)
		err := eng.IndexThrottled(req.Dir, engine.FastIndexOptions())

		endCount := getStoreCount(cfg.IndexPath)

		indexingMutex.Lock()
		isIndexing = false
		currentCount = endCount
		if err != nil {
			log.Printf("Onboarding Fast indexing error: %v", err)
		} else {
			log.Printf("Onboarding Fast indexing finished. Chunks: %d -> %d", startCount, endCount)

			indexedFoldersMutex.Lock()
			exists := false
			for _, f := range indexedFolders {
				if f == req.Dir {
					exists = true
					break
				}
			}
			if !exists {
				indexedFolders = append(indexedFolders, req.Dir)
			}
			indexedFoldersMutex.Unlock()
		}
		indexingMutex.Unlock()

		// V2: KHÔNG tự lan ra quét toàn home sau khi index thư mục user chọn.
		// User chọn thư mục nào → index đúng thư mục đó. Muốn thêm thư mục khác
		// thì user tự chọn tiếp (handleOnboard/handleIndex). Không nuốt home.
		if err == nil {
			log.Printf("Onboarding xong: đã index thư mục user chọn %s (không tự quét home).", req.Dir)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"started": true})
}

// requiredModelFile is a file that MUST exist with at least minSize bytes for the
// model to be usable. minSize guards against the classic failure: a download cut
// off mid-stream leaving a 0-byte / truncated file at the destination (the cause
// of "model.onnx_data = 0MB" + "failed to load tokenizer" on startup).
type requiredModelFile struct {
	relPath string // relative to ModelRoot
	minSize int64  // bytes; a sane lower bound, not the exact size
}

// requiredModelFiles is the single source of truth for "is the model complete".
// Sizes are conservative lower bounds (real files are larger) so a truncated
// download is caught without hard-coding exact byte counts.
var requiredModelFiles = []requiredModelFile{
	{"models/onnx/bge-m3/model.onnx", 100 * 1024},                  // ~725KB graph
	{"models/onnx/bge-m3/model.onnx_data", 2_000_000_000},          // ~2.3GB weights
	{"models/onnx/bge-m3/tokenizer.json", 1_000_000},               // ~17MB
	{"models/onnx/bge-m3/sentencepiece.bpe.model", 1_000_000},      // ~5MB
	{"models/onnx/bge-m3/config.json", 100},                        // ~687B
	{"models/onnx/bge-reranker/tokenizer.json", 1_000_000},         // ~17MB
}

// verifyModelIntegrity returns the list of required files that are missing or
// truncated under root. Empty slice = model is complete and usable. This is what
// "khách mở máy lên chạy OK" depends on: catch a half-broken ~/.sfs BEFORE the
// engine tries to load it and fails with a cryptic error.
func verifyModelIntegrity(root string) []string {
	var bad []string
	for _, rf := range requiredModelFiles {
		p := filepath.Join(root, rf.relPath)
		info, err := os.Stat(p)
		if err != nil {
			bad = append(bad, rf.relPath+" (thiếu)")
			continue
		}
		if info.Size() < rf.minSize {
			bad = append(bad, fmt.Sprintf("%s (cụt: %d < %d bytes)", rf.relPath, info.Size(), rf.minSize))
		}
	}
	return bad
}

// checkModelExists now requires ALL critical files present and non-truncated,
// not just model.onnx. A reranker int8 (model_int8.onnx) is optional; the engine
// falls back to FP32. The base files above are mandatory.
func checkModelExists(cfg engine.Config) bool {
	return len(verifyModelIntegrity(cfg.ModelRoot)) == 0
}

// cleanCorruptModelFiles xóa các file model CỤT (kích thước < ngưỡng) và mọi file
// .part còn sót. Lý do: nếu để file cụt ở đích, lần setup sau có thể "thấy file
// tồn tại" rồi bỏ qua → hỏng vĩnh viễn. Dọn đi để setup tải lại SẠCH. KHÔNG tự
// tải (không tốn băng thông sau lưng user) — chỉ dọn + để app báo cần setup.
// Trả số file đã dọn.
func cleanCorruptModelFiles(root string) int {
	cleaned := 0
	for _, rf := range requiredModelFiles {
		p := filepath.Join(root, rf.relPath)
		info, err := os.Stat(p)
		if err != nil {
			continue // thiếu hẳn — không có gì để dọn
		}
		if info.Size() < rf.minSize {
			if os.Remove(p) == nil {
				log.Printf("setup: đã dọn file cụt %s (%d bytes)", rf.relPath, info.Size())
				cleaned++
			}
		}
		// dọn luôn .part dở nếu có
		if os.Remove(p + ".part") == nil {
			cleaned++
		}
	}
	return cleaned
}

func getRemoteSize(url string) int64 {
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return 0
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return resp.ContentLength
	}
	return 0
}

// downloadFile tải ATOMIC: ghi vào <dest>.part, kiểm đủ byte so với
// Content-Length, RỒI mới rename sang dest. Nếu mạng đứt giữa chừng, chỉ có file
// .part dở (sẽ ghi đè lần sau), KHÔNG bao giờ để file cụt ở đích. Đây là sửa
// gốc rễ của lỗi "~/.sfs/model.onnx_data = 0MB / tokenizer.json thiếu".
func downloadFile(url string, destPath string, onWrite func(n int)) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP status error: %s", resp.Status)
	}
	expected := resp.ContentLength // -1 nếu server không báo

	partPath := destPath + ".part"
	out, err := os.Create(partPath)
	if err != nil {
		return err
	}

	var written int64
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				out.Close()
				os.Remove(partPath)
				return writeErr
			}
			written += int64(n)
			onWrite(n)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			out.Close()
			os.Remove(partPath)
			return readErr
		}
	}
	if err := out.Close(); err != nil {
		os.Remove(partPath)
		return err
	}

	// Verify: đủ byte như server hứa? (bắt trường hợp kết nối đóng sớm.)
	if expected > 0 && written != expected {
		os.Remove(partPath)
		return fmt.Errorf("tải thiếu %s: nhận %d/%d bytes (mạng đứt giữa chừng)", filepath.Base(destPath), written, expected)
	}

	// Atomic: đổi tên file hoàn chỉnh sang đích. Đích không bao giờ ở trạng thái dở.
	if err := os.Rename(partPath, destPath); err != nil {
		os.Remove(partPath)
		return err
	}
	return nil
}

func downloadAndSetupModels(cfg engine.Config) error {
	root := cfg.ModelRoot
	bgeM3Dir := filepath.Join(root, "models", "onnx", "bge-m3")
	bgeRerankerDir := filepath.Join(root, "models", "onnx", "bge-reranker")

	if err := os.MkdirAll(bgeM3Dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", bgeM3Dir, err)
	}
	if err := os.MkdirAll(bgeRerankerDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", bgeRerankerDir, err)
	}

	bgeM3Files := []struct {
		srcPath  string
		destName string
	}{
		{srcPath: "onnx/model.onnx", destName: "model.onnx"},
		{srcPath: "onnx/model.onnx_data", destName: "model.onnx_data"},
		{srcPath: "tokenizer.json", destName: "tokenizer.json"},
		{srcPath: "tokenizer_config.json", destName: "tokenizer_config.json"},
		{srcPath: "special_tokens_map.json", destName: "special_tokens_map.json"},
		{srcPath: "config.json", destName: "config.json"},
		{srcPath: "sentencepiece.bpe.model", destName: "sentencepiece.bpe.model"},
	}
	bgeM3BaseURL := "https://huggingface.co/BAAI/bge-m3/resolve/main/"

	rerankerFiles := []struct {
		srcPath  string
		destName string
	}{
		{srcPath: "onnx/model.onnx", destName: "model.onnx"},
		{srcPath: "onnx/model.onnx_data", destName: "model.onnx_data"},
		{srcPath: "tokenizer.json", destName: "tokenizer.json"},
		{srcPath: "tokenizer_config.json", destName: "tokenizer_config.json"},
		{srcPath: "special_tokens_map.json", destName: "special_tokens_map.json"},
		{srcPath: "config.json", destName: "config.json"},
	}
	bgeRerankerBaseURL := "https://huggingface.co/onnx-community/bge-reranker-v2-m3-ONNX/resolve/main/"

	type downloadItem struct {
		url      string
		destPath string
		destName string
		size     int64
	}

	var items []downloadItem
	for _, f := range bgeM3Files {
		items = append(items, downloadItem{
			url:      fmt.Sprintf("%s%s?download=true", bgeM3BaseURL, f.srcPath),
			destPath: filepath.Join(bgeM3Dir, f.destName),
			destName: "BGE-M3 " + f.destName,
		})
	}
	for _, f := range rerankerFiles {
		items = append(items, downloadItem{
			url:      fmt.Sprintf("%s%s?download=true", bgeRerankerBaseURL, f.srcPath),
			destPath: filepath.Join(bgeRerankerDir, f.destName),
			destName: "Reranker " + f.destName,
		})
	}

	var totalExpectedSize int64 = 0
	var alreadyDownloadedSize int64 = 0

	for i, item := range items {
		setupMutex.Lock()
		downloadStatus = "Đang kiểm tra: " + item.destName
		setupMutex.Unlock()

		size := getRemoteSize(item.url)
		items[i].size = size

		if size > 0 {
			if info, err := os.Stat(item.destPath); err == nil && info.Size() == size {
				alreadyDownloadedSize += size
			} else {
				totalExpectedSize += size
			}
		} else {
			// fallback
			totalExpectedSize += 100 * 1024 * 1024
		}
	}

	var currentDownloaded int64 = 0
	for _, item := range items {
		setupMutex.Lock()
		downloadStatus = "Đang tải: " + item.destName
		setupMutex.Unlock()

		if item.size > 0 {
			if info, err := os.Stat(item.destPath); err == nil && info.Size() == item.size {
				continue
			}
		}

		err := downloadFile(item.url, item.destPath, func(n int) {
			currentDownloaded += int64(n)
			setupMutex.Lock()
			total := totalExpectedSize + alreadyDownloadedSize
			if total > 0 {
				downloadPercent = int((alreadyDownloadedSize + currentDownloaded) * 100 / total)
			}
			setupMutex.Unlock()
		})
		if err != nil {
			if filepath.Base(item.destPath) == "special_tokens_map.json" {
				log.Printf("Skipping optional file %s due to error: %v", item.destName, err)
				continue
			}
			return fmt.Errorf("failed to download %s: %w", item.destName, err)
		}
	}

	// Verify cuối: sau khi tải, model PHẢI đủ + đúng kích thước. Nếu không (mạng
	// chập chờn để lại file cụt), báo lỗi RÕ thay vì để app load fail sau này.
	if bad := verifyModelIntegrity(root); len(bad) > 0 {
		return fmt.Errorf("tải model xong nhưng KHÔNG toàn vẹn, các file lỗi: %s — hãy chạy setup lại", strings.Join(bad, "; "))
	}
	log.Printf("setup: model toàn vẹn, đã kiểm %d file bắt buộc", len(requiredModelFiles))
	return nil
}

// StartServer starts the HTTP server on localhost:port and returns the server instance
// Server wraps the http.Server and handles graceful cleanup
type Server struct {
	httpServer *http.Server
}

func (s *Server) Shutdown() {
	log.Println("Shutting down background HTTP server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.httpServer.Shutdown(ctx); err != nil {
		log.Printf("Server Shutdown Failed: %v", err)
	}

	engineMutex.Lock()
	if globalEngine != nil {
		log.Println("Closing search engine...")
		if err := globalEngine.Close(); err != nil {
			log.Printf("Error closing engine: %v", err)
		}
	}
	engineMutex.Unlock()
}

// handlePredict trả top-5 file dự đoán cho ô search trống (khoảnh khắc "wow").
func handlePredict(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	engineMutex.RLock()
	eng := globalEngine
	engineMutex.RUnlock()
	if eng == nil || behaviorLog == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]intent.Prediction{})
		return
	}

	events, _ := behaviorLog.Load()
	now := time.Now()
	prof := intent.BuildProfileWithEmbed(events, now, eng.EmbedQuery)

	entries := eng.FileEntries()
	cands := make([]intent.FileCandidate, 0, len(entries))
	for _, fe := range entries {
		cands = append(cands, intent.FileCandidate{Path: fe.Path, Vector: fe.Vector, ModTime: fe.ModTime})
	}

	preds := intent.Predict(cands, prof, now, 5)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(preds)
}

// handleEvent nhận 1 event hành vi từ frontend và ghi vào behavior log (local).
// Im lặng nuốt lỗi ghi (không để hỏng UX vì log) nhưng trả 200 nếu nhận hợp lệ.
func handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var e intent.Event
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	if behaviorLog != nil {
		_ = behaviorLog.Append(e) // lỗi ghi không làm hỏng UX
	}
	w.WriteHeader(http.StatusOK)
}

// Start starts the HTTP server on localhost:port and returns the Server helper.
// It automatically initializes the engine if model files exist, and runs in background.
func Start(cfg engine.Config, port string) (*Server, error) {
	globalConfig = cfg
	behaviorLog = intent.NewLog(cfg.IndexPath)

	// Load state at startup
	stateMutex.Lock()
	globalState = loadState(cfg.IndexPath)
	onboarded := globalState.Onboarded
	stateMutex.Unlock()

	// Setup Engine if model files already exist
	if checkModelExists(cfg) {
		log.Printf("Models found on disk. Initializing search engine...")
		eng, err := engine.New(cfg)
		if err != nil {
			log.Printf("Failed to initialize engine on startup: %v", err)
		} else {
			globalEngine = eng
			log.Printf("Engine successfully initialized.")

			// V2: lúc khởi động chỉ REFRESH các thư mục user đã chọn (nhặt file
			// mới), KHÔNG quét toàn bộ home dir. "Mở máy lên" nhẹ, không giật.
			if onboarded {
				go refreshIndexedDirs(eng)
			}
		}
	} else {
		// Báo RÕ cái gì hỏng (thiếu file nào / cụt bao nhiêu) thay vì "missing"
		// mơ hồ rồi để engine load fail với lỗi khó hiểu. Đây là cốt lõi của
		// "khách mở máy lên thì biết ngay tình trạng".
		bad := verifyModelIntegrity(cfg.ModelRoot)
		log.Printf("WARNING: Model chưa sẵn sàng tại %s. Các file thiếu/hỏng: %s",
			cfg.ModelRoot, strings.Join(bad, "; "))
		// Tự DỌN file cụt/dở (không tự tải) để lần setup sau tải lại sạch, không
		// bị skip nhầm vì "file đã tồn tại". App vẫn báo user cần chạy setup.
		if n := cleanCorruptModelFiles(cfg.ModelRoot); n > 0 {
			log.Printf("setup: đã dọn %d file model hỏng — hãy chạy setup để tải lại model sạch.", n)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleHome)
	mux.HandleFunc("/assets/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		filePath := strings.TrimPrefix(path, "/")
		data, err := assetsFS.ReadFile(filePath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(filePath, ".css") {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		} else if strings.HasSuffix(filePath, ".js") {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		}
		w.Write(data)
	})
	mux.HandleFunc("/api/search", handleSearch)
	mux.HandleFunc("/api/index", handleIndex)
	mux.HandleFunc("/api/health", handleHealth)
	mux.HandleFunc("/api/setup", handleSetup)
	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("/api/folders", handleFolders)
	mux.HandleFunc("/api/onboard", handleOnboard)
	mux.HandleFunc("/api/event", handleEvent)
	mux.HandleFunc("/api/predict", handlePredict)

	server := &http.Server{
		Addr:    "localhost:" + port,
		Handler: mux,
	}

	go func() {
		log.Printf("Server starting on http://localhost:%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Could not listen on localhost:%s: %v", port, err)
		}
	}()

	return &Server{httpServer: server}, nil
}

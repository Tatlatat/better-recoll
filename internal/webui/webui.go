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
	}

	q := r.URL.Query().Get("q")
	if q == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SearchResponse{
			Exact:   []SearchResultItem{},
			Suggest: []SearchResultItem{},
		})
		return
	}

	results, err := eng.SearchRanked(q, 10)
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

	go func() {
		var opts engine.IndexOptions
		if indexMode == "fast" {
			opts = engine.FastIndexOptions()
		} else {
			opts = engine.BackgroundIndexOptions()
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
				primaryDir := globalState.PrimaryDir
				stateMutex.Unlock()
				if onboarded {
					go runBackgroundHomeIndexing(eng, primaryDir)
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

func findSafeTrees(dirPath string, primaryDir string) (bool, []string) {
	name := filepath.Base(dirPath)

	if primaryDir != "" && strings.EqualFold(filepath.Clean(dirPath), filepath.Clean(primaryDir)) {
		return false, nil
	}

	if strings.HasPrefix(name, ".") ||
		strings.EqualFold(name, "Library") ||
		strings.EqualFold(name, "System") ||
		strings.EqualFold(name, "node_modules") ||
		strings.EqualFold(name, "caches") ||
		strings.EqualFold(name, "cache") {
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
		opts.OnlyExtensions = []string{"pdf", "docx", "xlsx", "txt"}
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

		if err == nil {
			log.Println("Onboarding Fast phase completed. Starting background home indexing...")
			runBackgroundHomeIndexing(eng, req.Dir)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"started": true})
}

func checkModelExists(cfg engine.Config) bool {
	modelPath := filepath.Join(cfg.ModelRoot, "models", "onnx", "bge-m3", "model.onnx")
	_, err := os.Stat(modelPath)
	return err == nil
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

func downloadFile(url string, destPath string, onWrite func(n int)) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP status error: %s", resp.Status)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			_, writeErr := out.Write(buf[:n])
			if writeErr != nil {
				return writeErr
			}
			onWrite(n)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
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

// Start starts the HTTP server on localhost:port and returns the Server helper.
// It automatically initializes the engine if model files exist, and runs in background.
func Start(cfg engine.Config, port string) (*Server, error) {
	globalConfig = cfg

	// Load state at startup
	stateMutex.Lock()
	globalState = loadState(cfg.IndexPath)
	onboarded := globalState.Onboarded
	primaryDir := globalState.PrimaryDir
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

			// AUTOMATICALLY resume background home indexing if onboarded
			if onboarded {
				go runBackgroundHomeIndexing(eng, primaryDir)
			}
		}
	} else {
		log.Printf("WARNING: AI Models missing from %s. Engine will remain uninitialized until setup is run.", cfg.ModelRoot)
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

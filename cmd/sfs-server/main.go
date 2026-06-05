package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"sfs/internal/engine"
)

const htmlContent = `<!DOCTYPE html>
<html lang="vi">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>SFS Search Engine</title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=Plus+Jakarta+Sans:wght@300;400;500;600;700&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg-color: #0b0f19;
            --panel-bg: rgba(17, 24, 39, 0.7);
            --border-color: rgba(255, 255, 255, 0.08);
            --text-primary: #f3f4f6;
            --text-secondary: #9ca3af;
            --primary: #6366f1;
            --primary-glow: rgba(99, 102, 241, 0.15);
            --exact-color: #10b981;
            --exact-glow: rgba(16, 185, 129, 0.1);
            --suggest-color: #3b82f6;
            --suggest-glow: rgba(59, 130, 246, 0.1);
            --font-main: 'Plus Jakarta Sans', -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
        }
        
        * {
            box-sizing: border-box;
            margin: 0;
            padding: 0;
        }
        
        body {
            background-color: var(--bg-color);
            color: var(--text-primary);
            font-family: var(--font-main);
            min-height: 100vh;
            display: flex;
            flex-direction: column;
            align-items: center;
            padding: 2rem 1rem;
            overflow-x: hidden;
        }

        /* Ambient background glow */
        .ambient-bg {
            position: fixed;
            top: 0;
            left: 0;
            right: 0;
            bottom: 0;
            pointer-events: none;
            z-index: -1;
            background: 
                radial-gradient(circle at 10% 20%, rgba(99, 102, 241, 0.05) 0%, transparent 40%),
                radial-gradient(circle at 90% 80%, rgba(16, 185, 129, 0.05) 0%, transparent 40%);
        }

        .container {
            width: 100%;
            max-width: 1200px;
            display: flex;
            flex-direction: column;
            gap: 2rem;
        }

        header {
            text-align: center;
            margin-bottom: 1rem;
        }

        h1 {
            font-size: 2.5rem;
            font-weight: 700;
            background: linear-gradient(135deg, #fff 0%, #a5b4fc 100%);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            letter-spacing: -0.025em;
            margin-bottom: 0.5rem;
        }

        header p {
            color: var(--text-secondary);
            font-size: 0.95rem;
        }

        .search-container {
            position: relative;
            max-width: 700px;
            width: 100%;
            margin: 0 auto;
        }

        .search-input-wrapper {
            position: relative;
            display: flex;
            align-items: center;
        }

        .search-input {
            width: 100%;
            padding: 1.1rem 1.5rem 1.1rem 3.5rem;
            font-size: 1.1rem;
            font-family: var(--font-main);
            background: var(--panel-bg);
            border: 1px solid var(--border-color);
            border-radius: 9999px;
            color: var(--text-primary);
            outline: none;
            transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
            backdrop-filter: blur(12px);
            box-shadow: 0 4px 30px rgba(0, 0, 0, 0.2);
        }

        .search-input:focus {
            border-color: var(--primary);
            box-shadow: 0 0 0 4px var(--primary-glow), 0 4px 30px rgba(0, 0, 0, 0.3);
        }

        .search-icon {
            position: absolute;
            left: 1.3rem;
            width: 1.25rem;
            height: 1.25rem;
            color: var(--text-secondary);
            pointer-events: none;
            transition: color 0.3s;
        }

        .search-input:focus + .search-icon {
            color: var(--primary);
        }

        .loader {
            position: absolute;
            right: 1.5rem;
            width: 1.2rem;
            height: 1.2rem;
            border: 2px solid rgba(255, 255, 255, 0.1);
            border-top: 2px solid var(--primary);
            border-radius: 50%;
            animation: spin 0.8s linear infinite;
            opacity: 0;
            transition: opacity 0.2s;
        }

        .loader.visible {
            opacity: 1;
        }

        @keyframes spin {
            0% { transform: rotate(0deg); }
            100% { transform: rotate(360deg); }
        }

        .results-grid {
            display: grid;
            grid-template-columns: 1fr 1fr;
            gap: 2rem;
            margin-top: 1rem;
        }

        @media (max-width: 868px) {
            .results-grid {
                grid-template-columns: 1fr;
            }
        }

        .results-column {
            background: var(--panel-bg);
            border: 1px solid var(--border-color);
            border-radius: 16px;
            padding: 1.5rem;
            backdrop-filter: blur(8px);
            display: flex;
            flex-direction: column;
            gap: 1.25rem;
            min-height: 300px;
        }

        .column-header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            border-bottom: 1px solid var(--border-color);
            padding-bottom: 0.75rem;
            margin-bottom: 0.25rem;
        }

        .column-title {
            font-size: 1.1rem;
            font-weight: 700;
            letter-spacing: 0.05em;
            display: flex;
            align-items: center;
            gap: 0.5rem;
        }

        .column-title.exact {
            color: var(--exact-color);
        }

        .column-title.suggest {
            color: var(--suggest-color);
        }

        .badge-count {
            font-size: 0.8rem;
            padding: 0.15rem 0.6rem;
            border-radius: 999px;
            font-weight: 600;
        }

        .exact .badge-count {
            background: var(--exact-glow);
            border: 1px solid rgba(16, 185, 129, 0.2);
            color: var(--exact-color);
        }

        .suggest .badge-count {
            background: var(--suggest-glow);
            border: 1px solid rgba(59, 130, 246, 0.2);
            color: var(--suggest-color);
        }

        .results-list {
            display: flex;
            flex-direction: column;
            gap: 1rem;
        }

        .result-card {
            background: rgba(255, 255, 255, 0.02);
            border: 1px solid var(--border-color);
            border-radius: 12px;
            padding: 1rem;
            transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
            position: relative;
            overflow: hidden;
            display: flex;
            flex-direction: column;
            gap: 0.75rem;
        }

        .result-card:hover {
            transform: translateY(-2px);
            background: rgba(255, 255, 255, 0.04);
            border-color: rgba(255, 255, 255, 0.15);
            box-shadow: 0 10px 20px rgba(0, 0, 0, 0.2);
        }

        .result-header {
            display: flex;
            justify-content: space-between;
            align-items: flex-start;
            gap: 1rem;
        }

        .file-path-container {
            display: flex;
            align-items: center;
            gap: 0.5rem;
            max-width: 80%;
        }

        .file-path {
            font-size: 0.8rem;
            color: var(--text-secondary);
            word-break: break-all;
            font-family: monospace;
            line-height: 1.4;
        }

        .copy-btn {
            background: none;
            border: none;
            color: var(--text-secondary);
            cursor: pointer;
            padding: 2px;
            display: inline-flex;
            align-items: center;
            justify-content: center;
            border-radius: 4px;
            transition: all 0.2s;
        }

        .copy-btn:hover {
            color: var(--text-primary);
            background: rgba(255, 255, 255, 0.1);
        }

        .score-badge {
            font-size: 0.75rem;
            font-weight: 600;
            padding: 0.2rem 0.5rem;
            border-radius: 6px;
            font-family: monospace;
        }

        .exact .score-badge {
            background: var(--exact-glow);
            color: var(--exact-color);
            border: 1px solid rgba(16, 185, 129, 0.15);
        }

        .suggest .score-badge {
            background: var(--suggest-glow);
            color: var(--suggest-color);
            border: 1px solid rgba(59, 130, 246, 0.15);
        }

        .result-text {
            font-size: 0.9rem;
            line-height: 1.6;
            color: rgba(255, 255, 255, 0.85);
            white-space: pre-wrap;
            word-break: break-word;
        }

        .empty-state {
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            color: var(--text-secondary);
            text-align: center;
            padding: 3rem 1rem;
            gap: 0.5rem;
            height: 100%;
        }

        .empty-state svg {
            width: 2.5rem;
            height: 2.5rem;
            color: rgba(255, 255, 255, 0.1);
            margin-bottom: 0.5rem;
        }

        .toast {
            position: fixed;
            bottom: 2rem;
            background: rgba(16, 185, 129, 0.9);
            color: white;
            padding: 0.75rem 1.5rem;
            border-radius: 9999px;
            font-size: 0.9rem;
            font-weight: 500;
            box-shadow: 0 10px 15px -3px rgba(0,0,0,0.3);
            pointer-events: none;
            opacity: 0;
            transform: translateY(1rem);
            transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
            z-index: 100;
        }

        .toast.visible {
            opacity: 1;
            transform: translateY(0);
        }
    </style>
</head>
<body>
    <div class="ambient-bg"></div>
    <div class="container">
        <header>
            <h1>SFS Search</h1>
            <p>Wrapping Vector & BM25 Search Engine with Reranker</p>
        </header>

        <div class="search-container">
            <div class="search-input-wrapper">
                <input type="text" id="search-input" class="search-input" placeholder="Nhập từ khóa tìm kiếm..." autocomplete="off" autofocus>
                <svg class="search-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                    <circle cx="11" cy="11" r="8"></circle>
                    <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
                </svg>
                <div id="search-loader" class="loader"></div>
            </div>
        </div>

        <div class="results-grid">
            <!-- CHÍNH XÁC Column -->
            <div class="results-column exact">
                <div class="column-header">
                    <div class="column-title exact">
                        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"></path><polyline points="22 4 12 14.01 9 11.01"></polyline></svg>
                        CHÍNH XÁC
                        <span id="exact-count" class="badge-count" style="display:none">0</span>
                    </div>
                </div>
                <div id="exact-list" class="results-list">
                    <div class="empty-state">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></line></svg>
                        <div>Chưa có tìm kiếm</div>
                    </div>
                </div>
            </div>

            <!-- GỢI Ý Column -->
            <div class="results-column suggest">
                <div class="column-header">
                    <div class="column-title suggest">
                        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"></path><line x1="12" y1="9" x2="12" y2="13"></line><line x1="12" y1="17" x2="12.01" y2="17"></line></svg>
                        GỢI Ý
                        <span id="suggest-count" class="badge-count" style="display:none">0</span>
                    </div>
                </div>
                <div id="suggest-list" class="results-list">
                    <div class="empty-state">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></line></svg>
                        <div>Chưa có tìm kiếm</div>
                    </div>
                </div>
            </div>
        </div>
    </div>

    <div id="toast" class="toast">Đã sao chép đường dẫn file!</div>

    <script>
        const searchInput = document.getElementById('search-input');
        const loader = document.getElementById('search-loader');
        const exactList = document.getElementById('exact-list');
        const suggestList = document.getElementById('suggest-list');
        const exactCount = document.getElementById('exact-count');
        const suggestCount = document.getElementById('suggest-count');
        const toast = document.getElementById('toast');

        let debounceTimer;

        searchInput.addEventListener('input', () => {
            clearTimeout(debounceTimer);
            const query = searchInput.value.trim();
            if (!query) {
                resetUI();
                return;
            }
            loader.classList.add('visible');
            debounceTimer = setTimeout(() => {
                fetchResults(query);
            }, 250);
        });

        function showToast(message) {
            toast.textContent = message;
            toast.classList.add('visible');
            setTimeout(() => {
                toast.classList.remove('visible');
            }, 2000);
        }

        async function copyToClipboard(text) {
            try {
                await navigator.clipboard.writeText(text);
                showToast('Đã sao chép đường dẫn!');
            } catch (err) {
                // fallback
                const el = document.createElement('textarea');
                el.value = text;
                document.body.appendChild(el);
                el.select();
                document.execCommand('copy');
                document.body.removeChild(el);
                showToast('Đã sao chép đường dẫn!');
            }
        }

        function resetUI() {
            loader.classList.remove('visible');
            exactCount.style.display = 'none';
            suggestCount.style.display = 'none';
            
            const emptyHTML = '<div class="empty-state">' +
                '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></line></svg>' +
                '<div>Chưa có tìm kiếm</div>' +
                '</div>';
            exactList.innerHTML = emptyHTML;
            suggestList.innerHTML = emptyHTML;
        }

        async function fetchResults(query) {
            try {
                const response = await fetch('/api/search?q=' + encodeURIComponent(query));
                if (!response.ok) {
                    throw new Error('Mạng hoặc máy chủ gặp lỗi');
                }
                const data = await response.json();
                renderResults(data);
            } catch (err) {
                console.error(err);
                showErrorState();
            } finally {
                loader.classList.remove('visible');
            }
        }

        function showErrorState() {
            const errorHTML = '<div class="empty-state" style="color: #ef4444;">' +
                '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></line></svg>' +
                '<div>Lỗi kết nối máy chủ</div>' +
                '</div>';
            exactList.innerHTML = errorHTML;
            suggestList.innerHTML = errorHTML;
        }

        function renderResults(data) {
            const exact = data.exact || [];
            const suggest = data.suggest || [];

            exactCount.textContent = exact.length;
            exactCount.style.display = exact.length > 0 ? 'inline-block' : 'none';
            
            suggestCount.textContent = suggest.length;
            suggestCount.style.display = suggest.length > 0 ? 'inline-block' : 'none';

            if (exact.length === 0) {
                exactList.innerHTML = '<div class="empty-state">' +
                    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></line></svg>' +
                    '<div>Không có kết quả chính xác</div>' +
                    '</div>';
            } else {
                exactList.innerHTML = exact.map(item => createCardHTML(item)).join('');
            }

            if (suggest.length === 0) {
                suggestList.innerHTML = '<div class="empty-state">' +
                    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></line></svg>' +
                    '<div>Không có kết quả gợi ý</div>' +
                    '</div>';
            } else {
                suggestList.innerHTML = suggest.map(item => createCardHTML(item)).join('');
            }
        }

        function createCardHTML(item) {
            const escapedText = item.text
                .replace(/&/g, "&amp;")
                .replace(/</g, "&lt;")
                .replace(/>/g, "&gt;")
                .replace(/"/g, "&quot;")
                .replace(/'/g, "&#039;");
                
            const escapedPath = item.filePath
                .replace(/&/g, "&amp;")
                .replace(/</g, "&lt;")
                .replace(/>/g, "&gt;")
                .replace(/"/g, "&quot;")
                .replace(/'/g, "&#039;");

            const displayScore = typeof item.score === 'number' ? item.score.toFixed(4) : 'N/A';

            return '<div class="result-card">' +
                   '  <div class="result-header">' +
                   '    <div class="file-path-container">' +
                   '      <span class="file-path" title="' + escapedPath + '">' + escapedPath + '</span>' +
                   '      <button class="copy-btn" onclick="copyToClipboard(this.getAttribute(\'data-path\'))" data-path="' + escapedPath + '" title="Sao chép đường dẫn">' +
                   '        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path></svg>' +
                   '      </button>' +
                   '    </div>' +
                   '    <span class="score-badge">Score: ' + displayScore + '</span>' +
                   '  </div>' +
                   '  <div class="result-text">' + escapedText + '</div>' +
                   '</div>';
        }
    </script>
</body>
</html>`

func handleSearch(eng *engine.Engine) http.HandlerFunc {
	type SearchResultItem struct {
		FilePath string  `json:"filePath"`
		Text     string  `json:"text"`
		Score    float32 `json:"score"`
	}

	type SearchResponse struct {
		Exact   []SearchResultItem `json:"exact"`
		Suggest []SearchResultItem `json:"suggest"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
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
			http.Error(w, fmt.Sprintf("Search failed: %v", err), http.StatusInternalServerError)
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
}

func handleIndex(eng *engine.Engine) http.HandlerFunc {
	type IndexRequest struct {
		Dir string `json:"dir"`
	}

	type IndexResponse struct {
		Indexed bool `json:"indexed"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
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

		log.Printf("Indexing directory: %s", req.Dir)
		if err := eng.Index(req.Dir); err != nil {
			log.Printf("Indexing error: %v", err)
			http.Error(w, fmt.Sprintf("Indexing failed: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(IndexResponse{Indexed: true})
	}
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

func handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(htmlContent))
}

func main() {
	// (1) read model root via env SFS_ROOT or default (use engine.DefaultConfig with empty modelRoot to auto-resolve, index path = a .sfsindex under the model root or cwd).
	sfsRoot := os.Getenv("SFS_ROOT")
	var cfg engine.Config
	if sfsRoot != "" {
		absRoot, err := filepath.Abs(sfsRoot)
		if err != nil {
			log.Fatalf("Error determining absolute root path of SFS_ROOT: %v", err)
		}
		cfg = engine.DefaultConfig(absRoot, filepath.Join(absRoot, ".sfsindex"))
	} else {
		// Default case: use engine.DefaultConfig with empty modelRoot to auto-resolve,
		// and index path is a .sfsindex under the cwd.
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatalf("Error getting current working directory: %v", err)
		}
		cfg = engine.DefaultConfig("", filepath.Join(cwd, ".sfsindex"))
	}

	log.Printf("Initializing SFS Search Engine...")
	log.Printf("Model Root: %s", cfg.ModelRoot)
	log.Printf("Index Path: %s", cfg.IndexPath)

	// (2) create the engine
	eng, err := engine.New(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize search engine: %v", err)
	}

	// (3) start an HTTP server on localhost:8765 (configurable via env SFS_PORT)
	port := os.Getenv("SFS_PORT")
	if port == "" {
		port = "8765"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleHome)
	mux.HandleFunc("/api/search", handleSearch(eng))
	mux.HandleFunc("/api/index", handleIndex(eng))
	mux.HandleFunc("/api/health", handleHealth)

	server := &http.Server{
		Addr:    "localhost:" + port,
		Handler: mux,
	}

	// Set up graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Server starting on http://localhost:%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Could not listen on localhost:%s: %v", port, err)
		}
	}()

	<-stop
	log.Println("Shutting down server gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server Shutdown Failed: %v", err)
	}

	log.Println("Closing search engine...")
	if err := eng.Close(); err != nil {
		log.Printf("Error closing engine: %v", err)
	}

	log.Println("Server exited successfully")
}

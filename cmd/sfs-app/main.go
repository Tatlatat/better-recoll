package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"sfs/internal/engine"

	"github.com/getlantern/systray"
	"github.com/webview/webview_go"
	"golang.design/x/hotkey"
)

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
)

// Reuse the exact same HTML and handlers from cmd/sfs-server/main.go
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
            padding: 1.5rem 1rem;
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
            gap: 1.5rem;
        }

        /* Navigation styling */
        .main-nav {
            width: 100%;
            display: flex;
            align-items: center;
            justify-content: space-between;
            background: var(--panel-bg);
            border: 1px solid var(--border-color);
            border-radius: 9999px;
            padding: 0.5rem 1.5rem;
            backdrop-filter: blur(12px);
            box-shadow: 0 4px 30px rgba(0, 0, 0, 0.2);
        }

        .nav-brand {
            font-weight: 700;
            font-size: 0.95rem;
            letter-spacing: 0.1em;
            background: linear-gradient(135deg, #a5b4fc 0%, #6366f1 100%);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }

        .nav-links {
            display: flex;
            gap: 0.5rem;
        }

        .nav-link {
            background: none;
            border: none;
            color: var(--text-secondary);
            font-family: var(--font-main);
            font-size: 0.9rem;
            font-weight: 600;
            padding: 0.5rem 1.25rem;
            border-radius: 9999px;
            cursor: pointer;
            display: flex;
            align-items: center;
            gap: 0.4rem;
            transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
        }

        .nav-link:hover {
            color: var(--text-primary);
            background: rgba(255, 255, 255, 0.04);
        }

        .nav-link.active {
            color: white;
            background: var(--primary);
            box-shadow: 0 4px 12px var(--primary-glow);
        }

        /* Setup banner styling */
        .setup-banner {
            background: rgba(239, 68, 68, 0.07);
            border: 1px solid rgba(239, 68, 68, 0.2);
            border-radius: 12px;
            padding: 0.75rem 1.25rem;
            display: none;
            align-items: center;
            justify-content: space-between;
            width: 100%;
        }

        .setup-banner.visible {
            display: flex;
        }

        .setup-text {
            color: #fca5a5;
            font-size: 0.9rem;
            display: flex;
            align-items: center;
            gap: 0.5rem;
        }

        .setup-btn {
            background: #ef4444;
            color: white;
            border: none;
            padding: 0.4rem 1rem;
            border-radius: 6px;
            font-size: 0.85rem;
            font-weight: 600;
            cursor: pointer;
            transition: all 0.2s;
        }

        .setup-btn:hover {
            background: #dc2626;
            box-shadow: 0 0 10px rgba(239, 68, 68, 0.3);
        }

        .setup-btn:disabled {
            background: var(--text-secondary);
            cursor: not-allowed;
            box-shadow: none;
        }

        header {
            text-align: center;
            margin: 0.5rem 0;
        }

        h1 {
            font-size: 2.2rem;
            font-weight: 700;
            background: linear-gradient(135deg, #fff 0%, #a5b4fc 100%);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            letter-spacing: -0.025em;
            margin-bottom: 0.25rem;
        }

        header p {
            color: var(--text-secondary);
            font-size: 0.9rem;
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
            padding: 1rem 1.5rem 1rem 3.5rem;
            font-size: 1.05rem;
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
            gap: 1.5rem;
            margin-top: 0.5rem;
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
            font-size: 1.05rem;
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

        .view {
            display: flex;
            flex-direction: column;
            gap: 1.5rem;
        }
    </style>
</head>
<body>
    <div class="ambient-bg"></div>
    <div class="container">
        <!-- Navigation -->
        <nav class="main-nav">
            <div class="nav-brand">SFS BETTER RECOLL</div>
            <div class="nav-links">
                <button id="nav-search-btn" class="nav-link active" onclick="showView('search')">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><circle cx="11" cy="11" r="8"></circle><line x1="21" y1="21" x2="16.65" y2="16.65"></line></svg>
                    Tìm Kiếm
                </button>
                <button id="nav-setting-btn" class="nav-link" onclick="showView('setting')">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><circle cx="12" cy="12" r="3"></circle><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l-.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"></path></svg>
                    Thiết Lập
                </button>
            </div>
        </nav>

        <!-- VIEW 1: SEARCH VIEW -->
        <div class="view" id="search-view">
            <!-- Setup Warning Banner -->
            <div id="setup-banner" class="setup-banner">
                <div class="setup-text">
                    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"></path><line x1="12" y1="9" x2="12" y2="13"></line><line x1="12" y1="17" x2="12.01" y2="17"></line></svg>
                    <span id="setup-banner-msg">Chưa có model AI. Lập chỉ mục & tìm kiếm sẽ không hoạt động.</span>
                </div>
                <button id="setup-btn" class="setup-btn" onclick="startSetup()">Tải Ngay</button>
            </div>

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

        <!-- VIEW 2: SETTING VIEW -->
        <div class="view" id="setting-view" style="display:none">
            <!-- Index Group -->
            <div class="results-column" style="min-height: auto;">
                <div class="column-header">
                    <div class="column-title" style="color: var(--primary)">
                        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><circle cx="12" cy="12" r="3"></circle><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l-.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"></path></svg>
                        LẬP CHỈ MỤC THƯ MỤC
                    </div>
                </div>
                
                <div style="display: flex; flex-direction: column; gap: 1rem; margin-top: 1rem;">
                    <div style="display: flex; gap: 1rem; align-items: center; flex-wrap: wrap;">
                        <button class="setup-btn" style="background: var(--primary);" onclick="triggerFolderPicker()">Chọn thư mục...</button>
                        <span id="chosen-path" style="font-family: monospace; color: var(--text-secondary); word-break: break-all; flex-grow: 1; min-width: 200px;">Chưa chọn thư mục nào</span>
                    </div>
                    
                    <button id="index-btn" class="setup-btn" style="background: var(--exact-color); width: fit-content; display: none;" onclick="startIndexing()">Lập chỉ mục thư mục này</button>
                    
                    <div id="index-status" style="font-size: 0.9rem; color: var(--text-secondary); display: none;"></div>
                </div>
            </div>

            <!-- Folders List Group -->
            <div class="results-column" style="min-height: auto;">
                <div class="column-header">
                    <div class="column-title" style="color: var(--suggest-color)">
                        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path></svg>
                        THƯ MỤC ĐÃ LẬP CHỈ MỤC
                    </div>
                </div>
                
                <div id="folders-list" style="margin-top: 1rem; display: flex; flex-direction: column; gap: 0.75rem;">
                    <div style="color: var(--text-secondary); font-size: 0.9rem; font-style: italic;">Chưa có thư mục nào được lập chỉ mục trong phiên này.</div>
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
        let chosenPathStr = '';
        let isPollingStatus = false;

        // View Toggling
        function showView(viewName) {
            document.querySelectorAll('.view').forEach(v => v.style.display = 'none');
            document.querySelectorAll('.nav-link').forEach(l => l.classList.remove('active'));

            if (viewName === 'search') {
                document.getElementById('search-view').style.display = 'flex';
                document.getElementById('nav-search-btn').classList.add('active');
            } else if (viewName === 'setting') {
                document.getElementById('setting-view').style.display = 'flex';
                document.getElementById('nav-setting-btn').classList.add('active');
                fetchFolders();
            }
        }

        // Folder Picker via Webview Bind or prompt fallback
        async function triggerFolderPicker() {
            if (typeof window.pickFolder === 'function') {
                try {
                    const path = await window.pickFolder();
                    if (path) {
                        chosenPathStr = path;
                        document.getElementById('chosen-path').textContent = path;
                        document.getElementById('index-btn').style.display = 'inline-block';
                    }
                } catch (err) {
                    console.error('Lỗi chọn thư mục:', err);
                }
            } else {
                const path = prompt('Nhập đường dẫn thư mục:');
                if (path) {
                    chosenPathStr = path;
                    document.getElementById('chosen-path').textContent = path;
                    document.getElementById('index-btn').style.display = 'inline-block';
                }
            }
        }

        // Folder indexing request
        async function startIndexing() {
            if (!chosenPathStr) return;
            const btn = document.getElementById('index-btn');
            const statusDiv = document.getElementById('index-status');
            btn.disabled = true;
            statusDiv.style.display = 'block';
            statusDiv.textContent = 'Đang tiến hành lập chỉ mục...';
            statusDiv.style.color = 'var(--text-secondary)';

            try {
                const response = await fetch('/api/index', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json'
                    },
                    body: JSON.stringify({ dir: chosenPathStr })
                });

                if (!response.ok) {
                    const errMsg = await response.text();
                    throw new Error(errMsg || 'Lỗi không xác định');
                }

                statusDiv.textContent = 'Lập chỉ mục thành công!';
                statusDiv.style.color = 'var(--exact-color)';
                fetchFolders();
            } catch (err) {
                statusDiv.textContent = 'Lập chỉ mục thất bại: ' + err.message;
                statusDiv.style.color = '#ef4444';
            } finally {
                btn.disabled = false;
            }
        }

        // Fetch indexed folders
        async function fetchFolders() {
            try {
                const response = await fetch('/api/folders');
                if (response.ok) {
                    const folders = await response.json();
                    renderFolders(folders);
                }
            } catch (err) {
                console.error('Lỗi lấy danh sách thư mục:', err);
            }
        }

        function renderFolders(folders) {
            const container = document.getElementById('folders-list');
            if (!folders || folders.length === 0) {
                container.innerHTML = '<div style="color: var(--text-secondary); font-size: 0.9rem; font-style: italic;">Chưa có thư mục nào được lập chỉ mục trong phiên này.</div>';
                return;
            }

            container.innerHTML = folders.map(f => {
                const escaped = f
                    .replace(/&/g, "&amp;")
                    .replace(/</g, "&lt;")
                    .replace(/>/g, "&gt;")
                    .replace(/"/g, "&quot;")
                    .replace(/'/g, "&#039;");
                return '<div class="result-card" style="padding: 0.75rem 1rem; flex-direction: row; justify-content: space-between; align-items: center;">' +
                       '  <span style="font-family: monospace; font-size: 0.85rem; color: var(--text-primary); word-break: break-all;">' + escaped + '</span>' +
                       '  <span class="score-badge" style="background: var(--exact-glow); color: var(--exact-color); border-color: rgba(16, 185, 129, 0.2);">Indexed</span>' +
                       '</div>';
            }).join('');
        }

        // Search Input Handling
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
            }, 2505);
        }

        async function copyToClipboard(text) {
            try {
                await navigator.clipboard.writeText(text);
                showToast('Đã sao chép đường dẫn!');
            } catch (err) {
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
                    const data = await response.json();
                    if (data && data.error) {
                        throw new Error(data.error);
                    }
                    throw new Error('Mạng hoặc máy chủ gặp lỗi');
                }
                const data = await response.json();
                renderResults(data);
            } catch (err) {
                console.error(err);
                showErrorState(err.message);
            } finally {
                loader.classList.remove('visible');
            }
        }

        function showErrorState(msg) {
            const displayMsg = msg || 'Lỗi kết nối máy chủ';
            const errorHTML = '<div class="empty-state" style="color: #ef4444;">' +
                '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></line></svg>' +
                '<div>' + displayMsg + '</div>' +
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

        // Setup Banner / Status Polling Logic
        async function checkStatus() {
            try {
                const response = await fetch('/api/status');
                if (!response.ok) return;
                const status = await response.json();

                const banner = document.getElementById('setup-banner');
                const bannerMsg = document.getElementById('setup-banner-msg');
                const setupBtn = document.getElementById('setup-btn');

                if (status.missing) {
                    banner.classList.add('visible');
                    searchInput.disabled = true;
                    searchInput.placeholder = "Vui lòng tải model AI trước...";

                    if (status.downloading) {
                        bannerMsg.textContent = 'Đang tải model: ' + status.status + ' (' + status.percent + '%)';
                        setupBtn.style.display = 'none';
                        if (!isPollingStatus) {
                            startPolling();
                        }
                    } else if (status.error) {
                        bannerMsg.textContent = 'Lỗi tải: ' + status.error;
                        setupBtn.textContent = 'Thử Lại';
                        setupBtn.style.display = 'inline-block';
                        setupBtn.disabled = false;
                    } else {
                        bannerMsg.textContent = 'Chưa có model AI. Lập chỉ mục & tìm kiếm sẽ không hoạt động.';
                        setupBtn.textContent = 'Tải Ngay';
                        setupBtn.style.display = 'inline-block';
                        setupBtn.disabled = false;
                    }
                } else {
                    banner.classList.remove('visible');
                    searchInput.disabled = false;
                    searchInput.placeholder = "Nhập từ khóa tìm kiếm...";
                }
            } catch (err) {
                console.error('Lỗi kiểm tra trạng thái:', err);
            }
        }

        async function startSetup() {
            const setupBtn = document.getElementById('setup-btn');
            setupBtn.disabled = true;
            setupBtn.textContent = 'Đang chuẩn bị...';

            try {
                const response = await fetch('/api/setup', { method: 'POST' });
                if (response.ok) {
                    startPolling();
                } else {
                    const txt = await response.text();
                    alert('Lỗi bắt đầu setup: ' + txt);
                    setupBtn.disabled = false;
                    setupBtn.textContent = 'Tải Ngay';
                }
            } catch (err) {
                alert('Lỗi kết nối setup: ' + err.message);
                setupBtn.disabled = false;
                setupBtn.textContent = 'Tải Ngay';
            }
        }

        function startPolling() {
            if (isPollingStatus) return;
            isPollingStatus = true;
            const interval = setInterval(async () => {
                try {
                    const response = await fetch('/api/status');
                    if (response.ok) {
                        const status = await response.json();
                        const bannerMsg = document.getElementById('setup-banner-msg');
                        const setupBtn = document.getElementById('setup-btn');
                        const banner = document.getElementById('setup-banner');

                        if (status.downloading) {
                            bannerMsg.textContent = 'Đang tải model: ' + status.status + ' (' + status.percent + '%)';
                            setupBtn.style.display = 'none';
                        } else {
                            clearInterval(interval);
                            isPollingStatus = false;
                            
                            if (status.missing) {
                                if (status.error) {
                                    bannerMsg.textContent = 'Lỗi tải: ' + status.error;
                                    setupBtn.textContent = 'Thử Lại';
                                    setupBtn.style.display = 'inline-block';
                                    setupBtn.disabled = false;
                                } else {
                                    bannerMsg.textContent = 'Chưa có model AI. Lập chỉ mục & tìm kiếm sẽ không hoạt động.';
                                    setupBtn.textContent = 'Tải Ngay';
                                    setupBtn.style.display = 'inline-block';
                                    setupBtn.disabled = false;
                                }
                            } else {
                                banner.classList.remove('visible');
                                searchInput.disabled = false;
                                searchInput.placeholder = "Nhập từ khóa tìm kiếm...";
                                showToast('Đã tải và khởi tạo model AI thành công!');
                                resetUI();
                            }
                        }
                    }
                } catch (err) {
                    console.error('Lỗi poll status:', err);
                }
            }, 1000);
        }

        // Initialize
        checkStatus();
        fetchFolders();
    </script>
</body>
</html>`

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

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	engineMutex.RLock()
	eng := globalEngine
	engineMutex.RUnlock()

	if eng == nil {
		http.Error(w, "Mô hình AI chưa được tải. Hãy tải mô hình trước.", http.StatusServiceUnavailable)
		return
	}

	type IndexRequest struct {
		Dir string `json:"dir"`
	}

	type IndexResponse struct {
		Indexed bool `json:"indexed"`
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
		http.Error(w, fmt.Sprintf("Lập chỉ mục thất bại: %v", err), http.StatusInternalServerError)
		return
	}

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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(IndexResponse{Indexed: true})
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
	Missing     bool   `json:"missing"`
	Downloading bool   `json:"downloading"`
	Percent     int    `json:"percent"`
	Status      string `json:"status"`
	Error       string `json:"error"`
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	engineMutex.RLock()
	hasEngine := globalEngine != nil
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(htmlContent))
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

func main() {
	sfsRoot := os.Getenv("SFS_ROOT")
	var cfg engine.Config
	if sfsRoot != "" {
		absRoot, err := filepath.Abs(sfsRoot)
		if err != nil {
			log.Fatalf("Error determining absolute root path of SFS_ROOT: %v", err)
		}
		cfg = engine.DefaultConfig(absRoot, filepath.Join(absRoot, ".sfsindex"))
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatalf("Error getting current working directory: %v", err)
		}
		cfg = engine.DefaultConfig("", filepath.Join(cwd, ".sfsindex"))
	}

	port := os.Getenv("SFS_PORT")
	if port == "" {
		port = "8765"
	}

	globalConfig = cfg

	// Setup Engine if model files already exist
	if checkModelExists(cfg) {
		log.Printf("Models found on disk. Initializing search engine...")
		eng, err := engine.New(cfg)
		if err != nil {
			log.Printf("Failed to initialize engine on startup: %v", err)
		} else {
			globalEngine = eng
			log.Printf("Engine successfully initialized.")
		}
	} else {
		log.Printf("WARNING: AI Models missing from %s. Engine will remain uninitialized until setup is run.", cfg.ModelRoot)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleHome)
	mux.HandleFunc("/api/search", handleSearch)
	mux.HandleFunc("/api/index", handleIndex)
	mux.HandleFunc("/api/health", handleHealth)
	mux.HandleFunc("/api/setup", handleSetup)
	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("/api/folders", handleFolders)

	server := &http.Server{
		Addr:    "localhost:" + port,
		Handler: mux,
	}

	go func() {
		log.Printf("Starting background HTTP server on http://localhost:%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server ListenAndServe error: %v", err)
		}
	}()

	// Ensure the server shuts down when the app exits
	defer func() {
		log.Println("Shutting down background HTTP server...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
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
	}()

	// Open webview window
	w := webview.New(true)
	defer w.Destroy()

	w.SetTitle("better-recoll")
	w.SetSize(700, 500, webview.HintNone)

	// Bind folder picker
	w.Bind("pickFolder", func() string {
		cmd := exec.Command("osascript", "-e", `POSIX path of (choose folder with prompt "Chọn thư mục để lập chỉ mục")`)
		out, err := cmd.Output()
		if err != nil {
			// User cancelled or error occurred
			return ""
		}
		return strings.TrimSpace(string(out))
	})

	// Run systray in a goroutine
	go func() {
		onReady := func() {
			systray.SetTitle("🔍")
			systray.SetTooltip("SFS Search Engine")

			mOpen := systray.AddMenuItem("Mở tìm kiếm", "Mở tìm kiếm")
			mSetting := systray.AddMenuItem("Cài đặt", "Cài đặt")
			mQuit := systray.AddMenuItem("Thoát", "Thoát")

			for {
				select {
				case <-mOpen.ClickedCh:
					w.Dispatch(func() {
						w.Navigate("http://localhost:" + port)
						w.Eval("showView('search')")
						bringToFront()
					})
				case <-mSetting.ClickedCh:
					w.Dispatch(func() {
						w.Navigate("http://localhost:" + port)
						w.Eval("showView('setting')")
						bringToFront()
					})
				case <-mQuit.ClickedCh:
					systray.Quit()
				}
			}
		}

		onExit := func() {
			w.Dispatch(func() {
				w.Destroy()
				os.Exit(0)
			})
		}

		systray.Run(onReady, onExit)
	}()

	// Register global hotkey in a goroutine
	go func() {
		time.Sleep(1 * time.Second)

		hk := hotkey.New([]hotkey.Modifier{hotkey.ModCmd, hotkey.ModShift}, hotkey.KeySpace)
		if err := hk.Register(); err != nil {
			log.Printf("hotkey: failed to register: %v", err)
			return
		}
		defer hk.Unregister()

		log.Printf("hotkey registered: Cmd+Shift+Space")
		for range hk.Keydown() {
			w.Dispatch(func() {
				w.Navigate("http://localhost:" + port)
				w.Eval("showView('search')")
				bringToFront()
			})
		}
	}()

	w.Navigate("http://localhost:" + port)
	w.Run()
}

func bringToFront() {
	pid := os.Getpid()
	script := fmt.Sprintf(`tell application "System Events" to set frontmost of (first process whose unix id is %d) to true`, pid)
	cmd := exec.Command("osascript", "-e", script)
	_ = cmd.Run()
}

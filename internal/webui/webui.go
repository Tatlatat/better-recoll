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

const htmlContent = `<!DOCTYPE html>
<html lang="vi" data-theme="light">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>SFS Search Engine</title>
    <!-- Offline Local Assets -->
    <link rel="stylesheet" href="/assets/daisyui.css">
    <script src="/assets/tailwind.js"></script>
    <style>
        html {
            background: transparent !important;
        }
        
        body {
            font-family: -apple-system, "SF Pro Display", "SF Pro Text", system-ui, sans-serif;
            min-height: 100vh;
        }

        /* Search mode overrides for floating search bar */
        body.search-mode {
            background-color: transparent !important;
            background-image: none !important;
            padding: 0 !important;
            margin: 0 !important;
            overflow: hidden !important;
            display: block !important;
            width: 100vw !important;
            min-height: auto !important;
        }

        body.search-mode .container {
            width: 100% !important;
            max-width: 100% !important;
            background: transparent !important;
            border: none !important;
            box-shadow: none !important;
            backdrop-filter: none !important;
            -webkit-backdrop-filter: none !important;
            padding: 0 !important;
            margin: 0 !important;
            gap: 12px;
            display: flex;
            flex-direction: column;
        }

        body.search-mode .main-nav,
        body.search-mode header {
            display: none !important;
        }

        /* Custom glass distortion effects for WWDC liquid glass */
        .glass-panel::before,
        .search-input-wrapper::before,
        body.search-mode .results-container::before {
            content: '';
            position: absolute;
            top: 0; left: 0; right: 0; bottom: 0;
            z-index: -1;
            background: rgba(255, 255, 255, 0.10);
            backdrop-filter: blur(20px) saturate(180%) brightness(1.05);
            -webkit-backdrop-filter: blur(20px) saturate(180%) brightness(1.05);
            filter: url(#glass-distortion-subtle);
            border-radius: inherit;
            pointer-events: none;
        }

        /* Search input wrapper specific styles */
        .search-input-wrapper {
            position: relative;
            display: flex;
            align-items: center;
            border: 1px solid rgba(255, 255, 255, 0.18);
            border-radius: 14px;
            box-shadow: 
                inset 0 1px 0 rgba(255, 255, 255, 0.25),
                0 10px 30px rgba(0, 0, 0, 0.05);
            background: transparent;
            overflow: hidden;
            transition: all 0.4s cubic-bezier(0.22, 1, 0.36, 1);
        }

        body.search-mode .search-input-wrapper {
            border-radius: 9999px; /* Capsule */
            box-shadow: 
                inset 0 1px 0 rgba(255, 255, 255, 0.25),
                0 10px 40px rgba(0, 0, 0, 0.25);
            padding: 4px;
        }

        .search-input-wrapper:focus-within {
            border-color: rgba(10, 132, 255, 0.45);
            box-shadow: 
                inset 0 1px 0 rgba(255, 255, 255, 0.3),
                0 0 0 3px rgba(10, 132, 255, 0.3),
                0 10px 30px rgba(0, 0, 0, 0.08);
        }

        body.search-mode .search-input-wrapper:focus-within {
            box-shadow: 
                inset 0 1px 0 rgba(255, 255, 255, 0.3),
                0 0 0 3px rgba(10, 132, 255, 0.3),
                0 10px 40px rgba(0, 0, 0, 0.25);
        }

        body.search-mode .results-container {
            position: relative;
            background: transparent;
            border: 1px solid rgba(255, 255, 255, 0.18);
            border-radius: 18px;
            box-shadow: 
                inset 0 1px 0 rgba(255, 255, 255, 0.25),
                0 15px 45px rgba(0, 0, 0, 0.25);
            padding: 0;
            max-height: 480px;
            overflow: hidden;
            animation: slideInResults 0.4s cubic-bezier(0.22, 1, 0.36, 1) forwards;
            transform-origin: top center;
        }

        body.search-mode .results-scroll-area {
            max-height: 450px;
            overflow-y: auto;
            overflow-x: hidden;
            padding: 12px 16px;
        }

        body.search-mode .results-scroll-area::-webkit-scrollbar {
            width: 6px;
        }
        body.search-mode .results-scroll-area::-webkit-scrollbar-track {
            background: transparent;
        }
        body.search-mode .results-scroll-area::-webkit-scrollbar-thumb {
            background: rgba(0, 0, 0, 0.12);
            border-radius: 10px;
        }

        @keyframes slideInResults {
            from {
                opacity: 0;
                transform: translateY(-8px) scale(0.99);
            }
            to {
                opacity: 1;
                transform: translateY(0) scale(1);
            }
        }

        /* Result card animation delay styles */
        .result-card {
            animation: slideIn 0.35s cubic-bezier(0.22, 1, 0.36, 1) both;
        }

        @keyframes slideIn {
            from {
                opacity: 0;
                transform: translateY(8px);
            }
            to {
                opacity: 1;
                transform: translateY(0);
            }
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
                radial-gradient(circle at 20% 20%, rgba(10, 132, 255, 0.05) 0%, transparent 50%),
                radial-gradient(circle at 80% 80%, rgba(52, 199, 89, 0.05) 0%, transparent 50%);
        }

        /* Setup banner overrides to respect display: none */
        .setup-banner {
            display: none !important;
        }
        .setup-banner.visible {
            display: flex !important;
        }

        #search-loader.visible {
            opacity: 1 !important;
        }
    </style>
</head>
<body class="bg-base-200 text-base-content min-h-screen flex flex-col items-center px-6 py-8">
    <!-- SVG Filter for Liquid Glass Refraction (iOS 26 / macOS Tahoe WWDC 2025 style) -->
    <svg style="display:none">
        <filter id="glass-distortion">
            <feTurbulence type="fractalNoise" baseFrequency="0.008 0.008" numOctaves="2" seed="5" result="noise"/>
            <feGaussianBlur in="noise" stdDeviation="2" result="blur"/>
            <feDisplacementMap in="SourceGraphic" in2="blur" scale="40" xChannelSelector="R" yChannelSelector="G"/>
        </filter>
        <filter id="glass-distortion-subtle">
            <feTurbulence type="fractalNoise" baseFrequency="0.008 0.008" numOctaves="2" seed="5" result="noise"/>
            <feGaussianBlur in="noise" stdDeviation="2" result="blur"/>
            <feDisplacementMap in="SourceGraphic" in2="blur" scale="12" xChannelSelector="R" yChannelSelector="G"/>
        </filter>
    </svg>
    
    <div class="ambient-bg"></div>

    <!-- Onboarding Modal (Visible only if not onboarded) -->
    <div id="onboard-modal" class="modal modal-open" style="display:none">
        <div class="modal-box max-w-md flex flex-col items-center gap-4 text-center">
            <h2 class="text-2xl font-bold text-base-content">Chào mừng bạn đến với Better Recoll</h2>
            <p class="text-sm text-base-content/70">Chọn một thư mục chính chứa tài liệu của bạn để bắt đầu lập chỉ mục nhanh.</p>
            <div class="flex flex-col gap-3 items-center w-full mt-2">
                <button id="onboard-pick-btn" class="btn btn-primary" onclick="triggerOnboardPicker()">Chọn thư mục tài liệu...</button>
                <span id="onboard-path" class="font-mono text-xs text-base-content/60 break-all select-all">Chưa chọn thư mục nào</span>
                <button id="onboard-start-btn" class="btn btn-success" style="display: none;" onclick="startOnboarding()">Bắt đầu sử dụng</button>
            </div>
            <div id="onboard-status" class="mt-4 text-sm text-base-content/60 hidden"></div>
        </div>
    </div>

    <div class="container mx-auto px-4 py-4 max-w-[680px] flex flex-col gap-6">
        <!-- Navigation -->
        <div class="navbar bg-base-100/50 backdrop-blur border border-base-200 rounded-full px-4 shadow-sm flex justify-between items-center w-full main-nav">
            <div class="flex-1">
                <span class="text-sm font-bold tracking-wider text-base-content/85">SFS BETTER RECOLL</span>
            </div>
            <div class="flex-none gap-2">
                <div class="join">
                    <button id="nav-search-btn" class="btn btn-sm join-item btn-primary text-xs" onclick="showView('search')">
                        <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><circle cx="11" cy="11" r="8"></circle><line x1="21" y1="21" x2="16.65" y2="16.65"></line></svg>
                        Tìm kiếm
                    </button>
                    <button id="nav-setting-btn" class="btn btn-sm join-item btn-ghost text-xs" onclick="showView('setting')">
                        <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><circle cx="12" cy="12" r="3"></circle><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l-.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"></path></svg>
                        Cài đặt
                    </button>
                </div>
            </div>
        </div>

        <!-- Setup Warning Banner (Visible on both views if missing) -->
        <div id="setup-banner" class="alert alert-warning shadow-sm py-3 px-4 rounded-xl flex justify-between items-center w-full setup-banner">
            <div class="flex items-center gap-3">
                <svg class="w-5 h-5 text-warning-content shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"/></svg>
                <span id="setup-banner-msg" class="text-sm font-medium text-warning-content">Chưa có model AI — cần tải (khoảng 4GB).</span>
            </div>
            <button id="setup-btn" class="btn btn-sm btn-primary shrink-0" onclick="startSetup()">Tải ngay</button>
        </div>

        <!-- Global Indexing Indicator -->
        <div id="indexing-indicator" class="flex gap-2 items-center bg-info/15 border border-info/20 text-info px-4 py-2 rounded-full w-fit mx-auto shadow-sm animate-pulse mb-4 indexing-indicator" style="display:none">
            <span class="loading loading-spinner loading-xs text-info indexing-spinner"></span>
            <span id="indexing-status-text" class="text-sm font-medium">Đang lập chỉ mục...</span>
        </div>

        <!-- VIEW 1: SEARCH VIEW -->
        <div class="view flex flex-col gap-6" id="search-view">
            <header class="text-center mt-4 mb-2">
                <h1 class="text-4xl font-extrabold tracking-tight mb-2 text-base-content">SFS Search</h1>
                <p class="text-base-content/60 text-sm">Wrapping Vector & BM25 Search Engine with Reranker</p>
            </header>

            <div class="search-container">
                <div class="search-input-wrapper glass relative flex items-center">
                    <input type="text" id="search-input" class="input input-lg input-bordered w-full pl-14 font-light text-2xl bg-transparent focus:outline-none border-none text-base-content" placeholder="Nhập từ khóa tìm kiếm..." autocomplete="off" autofocus>
                    <svg class="search-icon absolute left-5 w-6 h-6 text-base-content/40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                        <circle cx="11" cy="11" r="8"></circle>
                        <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
                    </svg>
                    <div id="search-loader" class="loading loading-spinner loading-md absolute right-5 text-primary opacity-0 transition-opacity duration-200"></div>
                </div>
            </div>

            <div class="results-container">
                <div class="results-scroll-area">
                    <!-- CHÍNH XÁC Section -->
                    <div class="flex flex-col gap-3" id="exact-section" style="display:none">
                        <div class="flex items-center justify-between border-b pb-2 border-base-200">
                            <div class="flex items-center gap-2 text-xs font-bold uppercase tracking-wider text-success">
                                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"></path><polyline points="22 4 12 14.01 9 11.01"></polyline></svg>
                                CHÍNH XÁC
                                <span id="exact-count" class="badge badge-success badge-sm font-semibold" style="display:none">0</span>
                            </div>
                        </div>
                        <div id="exact-list" class="flex flex-col gap-2"></div>
                    </div>

                    <!-- GỢI Ý Section -->
                    <div class="flex flex-col gap-3 mt-4" id="suggest-section" style="display:none">
                        <div class="flex items-center justify-between border-b pb-2 border-base-200">
                            <div class="flex items-center gap-2 text-xs font-bold uppercase tracking-wider text-info">
                                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"></path><line x1="12" y1="9" x2="12" y2="13"></line><line x1="12" y1="17" x2="12.01" y2="17"></line></svg>
                                GỢI Ý
                                <span id="suggest-count" class="badge badge-info badge-sm font-semibold" style="display:none">0</span>
                            </div>
                        </div>
                        <div id="suggest-list" class="flex flex-col gap-2"></div>
                    </div>

                    <!-- Status/Empty message area -->
                    <div id="results-message" class="empty-state">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></line></svg>
                        <div>Chưa có tìm kiếm</div>
                    </div>
                </div>
            </div>
        </div>

        <!-- VIEW 2: SETTING VIEW -->
        <div class="view" id="setting-view" style="display:none">
            <!-- Index Group -->
            <div class="card bg-base-100/40 border border-base-200 p-6 shadow-sm flex flex-col gap-4">
                <div class="border-b pb-2 border-base-200">
                    <div class="flex items-center gap-2 text-sm font-bold tracking-wider text-primary">
                        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><circle cx="12" cy="12" r="3"></circle><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l-.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"></path></svg>
                        Lập chỉ mục thư mục
                    </div>
                </div>
                
                <div class="flex flex-col gap-4 mt-2">
                    <div class="flex gap-4 items-center flex-wrap">
                        <button class="btn btn-primary btn-sm" onclick="triggerFolderPicker()">Chọn thư mục...</button>
                        <span id="chosen-path" class="font-mono text-xs text-base-content/60 break-all select-all flex-1 min-w-[200px]">Chưa chọn thư mục nào</span>
                    </div>
                    
                    <button id="index-btn" class="btn btn-success btn-sm w-fit" style="display: none;" onclick="startIndexing()">Lập chỉ mục thư mục này</button>
                    
                    <div id="index-status" class="text-sm text-base-content/60" style="display: none;"></div>
                </div>
            </div>

            <!-- Folders List Group -->
            <div class="card bg-base-100/40 border border-base-200 p-6 shadow-sm flex flex-col gap-4">
                <div class="border-b pb-2 border-base-200">
                    <div class="flex items-center gap-2 text-sm font-bold tracking-wider text-info">
                        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path></svg>
                        Thư mục đã lập chỉ mục
                    </div>
                </div>
                
                <div id="folders-list" class="flex flex-col gap-3 mt-2">
                    <div class="text-base-content/50 text-sm italic">Chưa có thư mục nào được lập chỉ mục trong phiên này.</div>
                </div>
            </div>
        </div>
    </div>

    <!-- Detail Modal -->
    <div id="detail-modal" class="modal" onclick="closeDetailModal(event)">
        <div class="modal-box max-w-2xl relative flex flex-col gap-4" onclick="event.stopPropagation()">
            <div class="flex justify-between items-center border-b pb-3 border-base-200">
                <h3 class="text-lg font-bold text-base-content">Chi tiết tài liệu</h3>
                <button class="btn btn-sm btn-circle btn-ghost" onclick="hideDetailModal()">&times;</button>
            </div>
            <div class="flex flex-col gap-4 overflow-y-auto max-h-[60vh] pr-2">
                <div class="flex flex-col gap-1">
                    <span class="text-xs font-bold text-base-content/50 uppercase tracking-wider">Tên file:</span>
                    <span id="detail-filename" class="text-base font-semibold text-base-content"></span>
                </div>
                <div class="flex flex-col gap-1">
                    <span class="text-xs font-bold text-base-content/50 uppercase tracking-wider">Đường dẫn:</span>
                    <div class="flex gap-2 items-center">
                        <span id="detail-filepath" class="font-mono text-xs text-base-content bg-base-200 px-2 py-1 rounded border border-base-300 break-all select-all"></span>
                        <button class="btn btn-sm btn-square btn-ghost opacity-60 hover:opacity-100" id="detail-copy-btn" title="Sao chép đường dẫn">
                            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path></svg>
                        </button>
                    </div>
                </div>
                <div class="flex flex-col gap-1">
                    <span class="text-xs font-bold text-base-content/50 uppercase tracking-wider">Điểm số (Score):</span>
                    <span id="detail-score" class="font-mono text-xs text-base-content bg-base-200 px-2 py-1 rounded border border-base-300 w-fit"></span>
                </div>
                <div class="flex flex-col gap-1">
                    <span class="text-xs font-bold text-base-content/50 uppercase tracking-wider">Nội dung đoạn trích:</span>
                    <div id="detail-content" class="text-sm leading-relaxed text-base-content bg-base-200 border border-base-300 rounded-lg p-4 max-h-[300px] overflow-y-auto whitespace-pre-wrap break-words"></div>
                </div>
            </div>
        </div>
    </div>

    <!-- Toast -->
    <div id="toast" class="toast toast-end toast-bottom z-[9999] opacity-0 translate-y-4 transition-all duration-300 pointer-events-none">
        <div class="alert alert-info py-2 px-4 text-xs font-semibold shadow-md">
            <span>Đã sao chép đường dẫn file!</span>
        </div>
    </div>

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
        let isIndexingActive = false;

        // View Toggling
        function showView(viewName) {
            document.querySelectorAll('.view').forEach(v => v.style.display = 'none');
            
            const btnSearch = document.getElementById('nav-search-btn');
            const btnSetting = document.getElementById('nav-setting-btn');
            
            btnSearch.classList.remove('btn-primary');
            btnSearch.classList.add('btn-ghost');
            btnSetting.classList.remove('btn-primary');
            btnSetting.classList.add('btn-ghost');

            const nav = document.querySelector('.main-nav');
            const header = document.querySelector('header');

            if (viewName === 'search') {
                document.getElementById('search-view').style.display = 'flex';
                btnSearch.classList.add('btn-primary');
                btnSearch.classList.remove('btn-ghost');
                if (document.body.classList.contains('search-mode')) {
                    if (nav) nav.style.display = 'none';
                    if (header) header.style.display = 'none';
                }
                adjustWindowSize();
                setTimeout(() => {
                    if (searchInput) {
                        searchInput.focus();
                        searchInput.select();
                    }
                }, 50);
            } else if (viewName === 'setting') {
                if (document.body.classList.contains('search-mode')) {
                    document.body.classList.remove('search-mode');
                }
                document.getElementById('setting-view').style.display = 'flex';
                btnSetting.classList.add('btn-primary');
                btnSetting.classList.remove('btn-ghost');
                if (nav) nav.style.display = 'flex';
                if (header) header.style.display = 'block';
                if (typeof window.resizeWindow === 'function') {
                    window.resizeWindow(900, 650);
                }
                fetchFolders();
            }
        }

        // Dynamic height adjustment for spotlight window
        function adjustWindowSize() {
            const searchView = document.getElementById('search-view');
            if (searchView && searchView.style.display !== 'none' && document.body.classList.contains('search-mode')) {
                const container = document.querySelector('.container');
                if (container) {
                    let height = container.scrollHeight + 16;
                    if (height < 90) height = 90;
                    if (height > 600) height = 600;
                    if (typeof window.resizeWindow === 'function') {
                        window.resizeWindow(640, height);
                    }
                }
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
            statusDiv.style.color = '';

            try {
                const response = await fetch('/api/index', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json'
                    },
                    body: JSON.stringify({ dir: chosenPathStr, mode: 'background' })
                });

                if (!response.ok) {
                    const errMsg = await response.text();
                    throw new Error(errMsg || 'Lỗi không xác định');
                }

                const data = await response.json();
                if (data && data.busy) {
                    statusDiv.textContent = 'Máy chủ đang bận lập chỉ mục một thư mục khác!';
                    statusDiv.style.color = '#ef4444';
                    btn.disabled = false;
                    return;
                }

                statusDiv.textContent = 'Đã bắt đầu lập chỉ mục trong nền!';
                statusDiv.style.color = '#10b981';
                fetchFolders();
            } catch (err) {
                statusDiv.textContent = 'Lập chỉ mục thất bại: ' + err.message;
                statusDiv.style.color = '#ef4444';
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
                container.innerHTML = '<div class="text-base-content/50 text-sm italic">Chưa có thư mục nào được lập chỉ mục trong phiên này.</div>';
                return;
            }

            container.innerHTML = folders.map(f => {
                const escaped = f
                    .replace(/&/g, "&amp;")
                    .replace(/</g, "&lt;")
                    .replace(/>/g, "&gt;")
                    .replace(/"/g, "&quot;")
                    .replace(/'/g, "&#039;");
                return '<div class="card card-compact w-full bg-base-100/30 border border-base-200 p-3 flex-row justify-between items-center shadow-sm">' +
                       '  <span class="font-mono text-xs text-base-content select-all break-all pr-4">' + escaped + '</span>' +
                       '  <span class="badge badge-success badge-sm shrink-0">Indexed</span>' +
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
            const toastAlert = toast.querySelector('span');
            if (toastAlert) toastAlert.textContent = message;
            toast.classList.add('opacity-100', 'translate-y-0');
            toast.classList.remove('opacity-0', 'translate-y-4');
            setTimeout(() => {
                toast.classList.remove('opacity-100', 'translate-y-0');
                toast.classList.add('opacity-0', 'translate-y-4');
            }, 2500);
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
            
            const exactSection = document.getElementById('exact-section');
            const suggestSection = document.getElementById('suggest-section');
            const resultsMessage = document.getElementById('results-message');

            exactSection.style.display = 'none';
            suggestSection.style.display = 'none';
            resultsMessage.style.display = 'flex';
            resultsMessage.className = 'empty-state flex flex-col items-center justify-center text-center py-12 gap-2 text-base-content/50';
            resultsMessage.innerHTML = '<svg class="w-8 h-8 opacity-40" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></svg><div>Chưa có tìm kiếm</div>';

            const resultsContainer = document.querySelector('.results-container');
            if (resultsContainer) {
                if (document.body.classList.contains('search-mode')) {
                    resultsContainer.style.display = 'none';
                } else {
                    resultsContainer.style.display = 'flex';
                }
            }
            adjustWindowSize();
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
            const exactSection = document.getElementById('exact-section');
            const suggestSection = document.getElementById('suggest-section');
            const resultsMessage = document.getElementById('results-message');

            exactSection.style.display = 'none';
            suggestSection.style.display = 'none';
            resultsMessage.style.display = 'flex';
            resultsMessage.className = 'empty-state flex flex-col items-center justify-center text-center py-12 gap-2 text-error';
            resultsMessage.innerHTML = '<svg class="w-8 h-8 opacity-70" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></svg><div>' + displayMsg + '</div>';

            const resultsContainer = document.querySelector('.results-container');
            if (resultsContainer) {
                resultsContainer.style.display = 'flex';
            }
            adjustWindowSize();
        }

        function renderResults(data) {
            const exact = data.exact || [];
            const suggest = data.suggest || [];

            exactCount.textContent = exact.length;
            exactCount.style.display = exact.length > 0 ? 'inline-block' : 'none';
            
            suggestCount.textContent = suggest.length;
            suggestCount.style.display = suggest.length > 0 ? 'inline-block' : 'none';

            const totalResults = exact.length + suggest.length;

            const exactSection = document.getElementById('exact-section');
            const suggestSection = document.getElementById('suggest-section');
            const resultsMessage = document.getElementById('results-message');

            const resultsContainer = document.querySelector('.results-container');
            if (resultsContainer) {
                resultsContainer.style.display = 'flex';
            }

            if (totalResults === 0) {
                exactSection.style.display = 'none';
                suggestSection.style.display = 'none';
                resultsMessage.style.display = 'flex';
                resultsMessage.className = 'empty-state flex flex-col items-center justify-center text-center py-12 gap-2 text-base-content/50';
                if (isIndexingActive) {
                    resultsMessage.innerHTML = '<div class="text-primary font-medium">⏳ Đang lập chỉ mục thêm tài liệu, kết quả sẽ đầy đủ hơn sau...</div>';
                } else {
                    resultsMessage.innerHTML = '<svg class="w-8 h-8 opacity-40" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></svg><div>Không tìm thấy kết quả</div>';
                }
            } else {
                exactSection.style.display = exact.length > 0 ? 'flex' : 'none';
                suggestSection.style.display = suggest.length > 0 ? 'flex' : 'none';
                
                if (exact.length > 0) {
                    exactList.innerHTML = exact.map((item, index) => createCardHTML(item, index)).join('');
                } else {
                    exactList.innerHTML = '';
                }

                if (suggest.length > 0) {
                    suggestList.innerHTML = suggest.map((item, index) => createCardHTML(item, index + exact.length)).join('');
                } else {
                    suggestList.innerHTML = '';
                }

                if (isIndexingActive && totalResults < 3) {
                    resultsMessage.style.display = 'flex';
                    resultsMessage.className = 'alert alert-info py-2 px-4 shadow-sm text-sm font-medium mt-4';
                    resultsMessage.innerHTML = '<div class="text-info-content font-medium text-center w-full">⏳ Đang lập chỉ mục thêm tài liệu, kết quả sẽ đầy đủ hơn sau...</div>';
                } else {
                    resultsMessage.style.display = 'none';
                }
            }
            adjustWindowSize();
        }

        function createCardHTML(item, index) {
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

            const filename = escapedPath.split(/[/\\]/).pop();
            const displayScore = typeof item.score === 'number' ? item.score.toFixed(4) : 'N/A';

            const jsonStr = JSON.stringify({
                filename: filename,
                filepath: item.filePath,
                score: displayScore,
                text: item.text
            }).replace(/'/g, "&#39;").replace(/"/g, "&quot;");

            const delay = typeof index === 'number' ? index * 40 : 0;

            return '<div class="card card-compact w-full bg-base-100/40 border border-base-200 hover:border-primary/40 hover:bg-base-100/60 shadow-sm hover:shadow transition-all duration-300 cursor-pointer result-card" style="animation-delay: ' + delay + 'ms" onclick="showDetailFromJSON(this.querySelector(\'.filename-link\').getAttribute(\'data-json\'))">' +
                   '  <div class="card-body gap-2">' +
                   '    <div class="flex flex-col gap-1">' +
                   '      <a class="filename-link text-base font-semibold text-base-content hover:text-primary transition-colors" onclick="event.stopPropagation(); showDetailFromJSON(this.getAttribute(\'data-json\'))" data-json="' + jsonStr + '" title="Xem chi tiết">' + filename + '</a>' +
                   '      <div class="flex items-center gap-2 text-xs text-base-content/60 font-mono">' +
                   '        <span class="file-path-text truncate max-w-[85%] inline-block" title="' + escapedPath + '">' + escapedPath + '</span>' +
                   '        <button class="btn btn-xs btn-square btn-ghost opacity-60 hover:opacity-100" onclick="event.stopPropagation(); copyToClipboard(this.getAttribute(\'data-path\'))" data-path="' + escapedPath + '" title="Sao chép đường dẫn">' +
                   '          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path></svg>' +
                   '        </button>' +
                   '      </div>' +
                   '    </div>' +
                   '    <div class="result-text text-sm text-base-content/85 whitespace-pre-wrap break-words">' + escapedText + '</div>' +
                   '    <div class="card-actions justify-start mt-1">' +
                   '      <span class="badge badge-sm badge-outline font-mono text-[10px] text-base-content/60">Score: ' + displayScore + '</span>' +
                   '    </div>' +
                   '  </div>' +
                   '</div>';
        }

        function showDetail(filename, filepath, score, text) {
            document.getElementById('detail-filename').textContent = filename;
            document.getElementById('detail-filepath').textContent = filepath;
            document.getElementById('detail-score').textContent = score;
            document.getElementById('detail-content').textContent = text;
            
            const copyBtn = document.getElementById('detail-copy-btn');
            copyBtn.onclick = () => {
                copyToClipboard(filepath);
            };

            const detailModal = document.getElementById('detail-modal');
            detailModal.classList.add('modal-open');
            detailModal.style.display = 'flex';
        }

        // Setup Banner / Status Polling Logic
        let wasDownloading = false;
        let wasMissing = true;

        async function checkStatus() {
            try {
                const response = await fetch('/api/status');
                if (!response.ok) return;
                const status = await response.json();

                isIndexingActive = (status.indexing === true) || (status.phase === 'background' || status.phase === 'fast');

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
                        wasDownloading = true;
                    } else if (status.error) {
                        bannerMsg.textContent = 'Lỗi tải: ' + status.error;
                        setupBtn.textContent = 'Thử lại';
                        setupBtn.style.display = 'inline-block';
                        setupBtn.disabled = false;
                        wasDownloading = false;
                    } else {
                        bannerMsg.textContent = 'Chưa có model AI — cần tải (khoảng 4GB).';
                        setupBtn.textContent = 'Tải ngay';
                        setupBtn.style.display = 'inline-block';
                        setupBtn.disabled = false;
                        wasDownloading = false;
                    }
                    wasMissing = true;
                } else {
                    banner.classList.remove('visible');
                    searchInput.disabled = false;
                    searchInput.placeholder = "Nhập từ khóa tìm kiếm...";

                    if (wasMissing) {
                        wasMissing = false;
                        if (wasDownloading) {
                            showToast('Đã tải và khởi tạo model AI thành công!');
                            resetUI();
                        }
                    }
                    wasDownloading = false;
                }

                // Handle Onboarding modal visibility
                const onboardModal = document.getElementById('onboard-modal');
                const onboardStatus = document.getElementById('onboard-status');
                const onboardPickBtn = document.getElementById('onboard-pick-btn');
                const onboardStartBtn = document.getElementById('onboard-start-btn');

                if (!status.onboarded) {
                    onboardModal.classList.add('modal-open');
                    onboardModal.style.display = 'flex';
                    if (status.indexing) {
                        onboardPickBtn.style.display = 'none';
                        onboardStartBtn.style.display = 'none';
                        onboardStatus.style.display = 'block';
                        onboardStatus.classList.remove('hidden');
                        onboardStatus.textContent = 'Đang thiết lập ban đầu (nhanh): ' + status.currentDir + ' (' + status.filesIndexed + ' file)...';
                    }
                } else {
                    onboardModal.classList.remove('modal-open');
                    onboardModal.style.display = 'none';
                }

                // Handle indexing status
                const indicator = document.getElementById('indexing-indicator');
                const indicatorText = document.getElementById('indexing-status-text');
                const indexStatusDiv = document.getElementById('index-status');
                const indexBtn = document.getElementById('index-btn');

                if (status.indexing) {
                    indicator.style.display = 'flex';
                    let prefix = 'Đang lập chỉ mục: ';
                    if (status.phase === 'background') {
                        prefix = 'Đang lập chỉ mục nền: ';
                    } else if (status.phase === 'fast') {
                        prefix = 'Đang lập chỉ mục nhanh: ';
                    }
                    const msg = prefix + status.currentDir + ' (' + status.filesIndexed + ' file)...';
                    indicatorText.textContent = msg;

                    if (indexStatusDiv) {
                        indexStatusDiv.style.display = 'block';
                        indexStatusDiv.textContent = msg;
                        indexStatusDiv.style.color = '';
                    }
                    if (indexBtn) {
                        indexBtn.disabled = true;
                        indexBtn.style.opacity = '0.5';
                    }
                } else {
                    indicator.style.display = 'none';
                    if (indexStatusDiv && indexStatusDiv.style.display === 'block' && indexStatusDiv.textContent.includes('Đang lập chỉ mục:')) {
                        const lastDir = indexStatusDiv.textContent.match(/Đang lập chỉ mục:\s*(.+?)\s*\(/);
                        const folderPath = (lastDir && lastDir[1]) || chosenPathStr || 'thư mục';
                        indexStatusDiv.textContent = 'Xong! Đã lập chỉ mục ' + folderPath;
                        indexStatusDiv.style.color = '#10b981';
                        setTimeout(() => {
                            if (indexStatusDiv.textContent.startsWith('Xong!')) {
                                indexStatusDiv.style.display = 'none';
                            }
                        }, 8000);
                    }
                    if (indexBtn) {
                        indexBtn.disabled = !chosenPathStr;
                        indexBtn.style.opacity = chosenPathStr ? '1' : '0.5';
                    }
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
                    checkStatus();
                } else {
                    const txt = await response.text();
                    alert('Lỗi bắt đầu setup: ' + txt);
                    setupBtn.disabled = false;
                    setupBtn.textContent = 'Tải ngay';
                }
            } catch (err) {
                alert('Lỗi kết nối setup: ' + err.message);
                setupBtn.disabled = false;
                setupBtn.textContent = 'Tải ngay';
            }
        }

        // Onboarding handlers
        let onboardPathStr = '';

        async function triggerOnboardPicker() {
            if (typeof window.pickFolder === 'function') {
                try {
                    const path = await window.pickFolder();
                    if (path) {
                        onboardPathStr = path;
                        document.getElementById('onboard-path').textContent = path;
                        document.getElementById('onboard-start-btn').style.display = 'inline-block';
                    }
                } catch (err) {
                    console.error('Lỗi chọn thư mục onboarding:', err);
                }
            } else {
                const path = prompt('Nhập đường dẫn thư mục tài liệu chính:');
                if (path) {
                    onboardPathStr = path;
                    document.getElementById('onboard-path').textContent = path;
                    document.getElementById('onboard-start-btn').style.display = 'inline-block';
                }
            }
        }

        async function startOnboarding() {
            if (!onboardPathStr) return;
            const pickBtn = document.getElementById('onboard-pick-btn');
            const startBtn = document.getElementById('onboard-start-btn');
            const statusDiv = document.getElementById('onboard-status');

            pickBtn.style.display = 'none';
            startBtn.style.display = 'none';
            statusDiv.style.display = 'block';
            statusDiv.classList.remove('hidden');
            statusDiv.textContent = 'Đang bắt đầu lập chỉ mục nhanh...';

            try {
                const response = await fetch('/api/onboard', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ dir: onboardPathStr })
                });

                if (!response.ok) {
                    const errMsg = await response.text();
                    throw new Error(errMsg || 'Lỗi không xác định');
                }
            } catch (err) {
                statusDiv.textContent = 'Lập chỉ mục nhanh thất bại: ' + err.message;
                pickBtn.style.display = 'inline-block';
                startBtn.style.display = 'inline-block';
            }
        }

        // Initialize
        checkStatus();
        fetchFolders();
        setInterval(checkStatus, 1000);
    </script>
</body>
</html>
`

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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(htmlContent))
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

			stateMutex.Lock()
			globalState.Onboarded = true
			globalState.PrimaryDir = req.Dir
			saveState(cfg.IndexPath, globalState)
			stateMutex.Unlock()

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

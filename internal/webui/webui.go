package webui

import (
	"context"
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
            --bg-color: #f5f5f7;
            --text-primary: #1d1d1f;
            --text-secondary: #86868b;
            --primary: #0A84FF; /* Apple blue accent */
            --primary-glow: rgba(10, 132, 255, 0.15);
            --exact-color: #34c759;
            --exact-glow: rgba(52, 199, 89, 0.15);
            --suggest-color: #0A84FF;
            --suggest-glow: rgba(10, 132, 255, 0.05);
            --font-main: -apple-system, "SF Pro Display", "SF Pro Text", system-ui, sans-serif;
            --spring-transition: all 0.4s cubic-bezier(0.22, 1, 0.36, 1);
        }
        
        * {
            box-sizing: border-box;
            margin: 0;
            padding: 0;
        }

        html {
            background: transparent !important;
        }
        
        body {
            background-color: var(--bg-color);
            color: var(--text-primary);
            font-family: var(--font-main);
            min-height: 100vh;
            display: flex;
            flex-direction: column;
            align-items: center;
            padding: 2rem 1.5rem;
            overflow-x: hidden;
            -webkit-font-smoothing: antialiased;
            -moz-osx-font-smoothing: grayscale;
            transition: background-color 0.3s;
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

        .container {
            width: 100%;
            max-width: 680px;
            display: flex;
            flex-direction: column;
            gap: 1.5rem;
            margin: 0 auto;
            transition: max-width 0.4s cubic-bezier(0.22, 1, 0.36, 1);
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

        /* Glass Panel Base Styles with pseudo-element filter to prevent text blur */
        .glass-panel {
            position: relative;
            background: transparent;
            border: 1px solid rgba(255, 255, 255, 0.18);
            border-radius: 18px;
            box-shadow: 
                inset 0 1px 0 rgba(255, 255, 255, 0.25),
                0 10px 40px rgba(0, 0, 0, 0.25);
            overflow: hidden;
        }

        .glass-panel::before {
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

        /* Navigation styling */
        .main-nav {
            position: relative;
            width: 100%;
            display: flex;
            align-items: center;
            justify-content: space-between;
            background: transparent;
            border: 1px solid rgba(255, 255, 255, 0.18);
            border-radius: 9999px;
            padding: 0.5rem 1.25rem;
            box-shadow: 
                inset 0 1px 0 rgba(255, 255, 255, 0.25),
                0 10px 30px rgba(0, 0, 0, 0.05);
            overflow: hidden;
            transition: var(--spring-transition);
        }

        .main-nav::before {
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

        .nav-brand {
            font-weight: 700;
            font-size: 0.95rem;
            letter-spacing: -0.01em;
            background: linear-gradient(135deg, #1d1d1f 0%, var(--primary) 100%);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }

        .nav-links {
            display: flex;
            gap: 0.25rem;
        }

        .nav-link {
            background: none;
            border: none;
            color: var(--text-secondary);
            font-family: var(--font-main);
            font-size: 0.85rem;
            font-weight: 500;
            padding: 0.4rem 1rem;
            border-radius: 9999px;
            cursor: pointer;
            display: flex;
            align-items: center;
            gap: 0.4rem;
            transition: var(--spring-transition);
        }

        .nav-link:hover {
            color: var(--text-primary);
            background: rgba(0, 0, 0, 0.04);
        }

        .nav-link.active {
            color: white;
            background: var(--primary);
            box-shadow: 0 4px 12px var(--primary-glow);
        }

        /* Setup banner styling */
        .setup-banner {
            position: relative;
            background: transparent;
            border: 1px solid rgba(255, 59, 48, 0.25);
            border-radius: 14px;
            padding: 0.75rem 1.25rem;
            display: none;
            align-items: center;
            justify-content: space-between;
            width: 100%;
            box-shadow: 
                inset 0 1px 0 rgba(255, 255, 255, 0.2),
                0 10px 30px rgba(0, 0, 0, 0.08);
            overflow: hidden;
            transition: var(--spring-transition);
        }

        .setup-banner::before {
            content: '';
            position: absolute;
            top: 0; left: 0; right: 0; bottom: 0;
            z-index: -1;
            background: rgba(255, 59, 48, 0.08);
            backdrop-filter: blur(20px) saturate(180%) brightness(1.05);
            -webkit-backdrop-filter: blur(20px) saturate(180%) brightness(1.05);
            filter: url(#glass-distortion-subtle);
            border-radius: inherit;
            pointer-events: none;
        }

        .setup-banner.visible {
            display: flex;
        }

        .setup-text {
            color: #ff3b30;
            font-size: 0.85rem;
            font-weight: 500;
            display: flex;
            align-items: center;
            gap: 0.5rem;
        }

        .setup-btn {
            background: var(--primary);
            color: white;
            border: none;
            padding: 0.5rem 1.25rem;
            border-radius: 8px;
            font-size: 13px;
            font-weight: 600;
            cursor: pointer;
            transition: var(--spring-transition);
            box-shadow: 0 1px 2px rgba(0, 0, 0, 0.05);
            display: inline-flex;
            align-items: center;
            gap: 0.35rem;
        }

        .setup-btn:hover {
            background: #0077ed;
            transform: translateY(-0.5px);
            box-shadow: 0 4px 12px rgba(10, 132, 255, 0.25);
        }

        .setup-btn:active {
            transform: translateY(0);
        }

        .setup-btn:disabled {
            background: #aeaeae;
            opacity: 0.6;
            cursor: not-allowed;
            box-shadow: none;
            transform: none;
        }

        header {
            text-align: center;
            margin: 1rem 0 0.5rem;
        }

        h1 {
            font-size: 2.4rem;
            font-weight: 700;
            letter-spacing: -0.03em;
            background: linear-gradient(135deg, #1d1d1f 30%, var(--primary) 100%);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            margin-bottom: 0.35rem;
        }

        header p {
            color: var(--text-secondary);
            font-size: 0.95rem;
            font-weight: 400;
            letter-spacing: -0.01em;
        }

        .search-container {
            position: relative;
            width: 100%;
            margin: 0 auto;
        }

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
            transition: var(--spring-transition);
        }

        .search-input-wrapper::before {
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

        body.search-mode .search-input-wrapper {
            border-radius: 9999px; /* Capsule */
            box-shadow: 
                inset 0 1px 0 rgba(255, 255, 255, 0.25),
                0 10px 40px rgba(0, 0, 0, 0.25);
            padding: 4px;
        }

        .search-input {
            width: 100%;
            padding: 1.1rem 1.5rem 1.1rem 3.75rem;
            font-size: 21px;
            font-weight: 300;
            letter-spacing: -0.015em;
            font-family: var(--font-main);
            background: transparent !important;
            border: none !important;
            box-shadow: none !important;
            backdrop-filter: none !important;
            -webkit-backdrop-filter: none !important;
            color: var(--text-primary);
            outline: none;
        }

        body.search-mode .search-input {
            padding: 0.85rem 1.5rem 0.85rem 3.5rem;
            font-size: 20px;
            border-radius: 9999px;
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

        .search-icon {
            position: absolute;
            left: 1.4rem;
            width: 1.35rem;
            height: 1.35rem;
            color: var(--text-secondary);
            pointer-events: none;
            transition: color 0.3s;
            z-index: 2;
        }

        .search-input-wrapper:focus-within .search-icon {
            color: var(--primary);
        }

        body.search-mode .search-icon {
            left: 1.25rem;
            width: 1.3rem;
            height: 1.3rem;
        }

        .loader {
            position: absolute;
            right: 1.5rem;
            width: 1.2rem;
            height: 1.2rem;
            border: 2px solid rgba(0, 0, 0, 0.05);
            border-top: 2px solid var(--primary);
            border-radius: 50%;
            animation: spin 0.8s linear infinite;
            opacity: 0;
            transition: opacity 0.2s;
            z-index: 2;
        }

        .loader.visible {
            opacity: 1;
        }

        @keyframes spin {
            0% { transform: rotate(0deg); }
            100% { transform: rotate(360deg); }
        }

        .results-container {
            display: flex;
            flex-direction: column;
            gap: 1.5rem;
            width: 100%;
            margin-top: 0.5rem;
            transition: var(--spring-transition);
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

        .results-scroll-area {
            width: 100%;
            display: flex;
            flex-direction: column;
            gap: 1.5rem;
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

        .results-column {
            position: relative;
            background: transparent;
            border: 1px solid rgba(255, 255, 255, 0.18);
            border-radius: 18px;
            padding: 1.5rem;
            display: flex;
            flex-direction: column;
            gap: 1.25rem;
            box-shadow: 
                inset 0 1px 0 rgba(255, 255, 255, 0.25),
                0 10px 40px rgba(0, 0, 0, 0.25);
            overflow: hidden;
            transition: var(--spring-transition);
        }

        .results-column::before {
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

        .results-section {
            display: flex;
            flex-direction: column;
            gap: 0.75rem;
            margin-bottom: 0.5rem;
        }

        .column-header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            border-bottom: 1px solid rgba(255, 255, 255, 0.1);
            padding-bottom: 0.5rem;
            margin-bottom: 0.25rem;
        }

        .column-title {
            font-size: 11px;
            font-weight: 700;
            letter-spacing: 0.08em;
            display: flex;
            align-items: center;
            gap: 0.4rem;
            text-transform: uppercase;
        }

        .column-title.exact {
            color: var(--exact-color);
        }

        .column-title.suggest {
            color: var(--primary);
        }

        .badge-count {
            font-size: 10px;
            padding: 0.1rem 0.5rem;
            border-radius: 999px;
            font-weight: 600;
        }

        .exact .badge-count {
            background: rgba(52, 199, 89, 0.15);
            border: 1px solid rgba(52, 199, 89, 0.25);
            color: var(--exact-color);
        }

        .suggest .badge-count {
            background: var(--primary-glow);
            border: 1px solid rgba(10, 132, 255, 0.25);
            color: var(--primary);
        }

        .results-list {
            display: flex;
            flex-direction: column;
            gap: 0.5rem;
        }

        .result-card {
            position: relative;
            background: transparent;
            border: 1px solid rgba(255, 255, 255, 0.15);
            border-radius: 12px;
            padding: 0.85rem 1.1rem;
            display: flex;
            flex-direction: column;
            gap: 0.35rem;
            cursor: pointer;
            box-shadow: 
                inset 0 1px 0 rgba(255, 255, 255, 0.15);
            overflow: hidden;
            transition: var(--spring-transition);
            animation: slideIn 0.35s cubic-bezier(0.22, 1, 0.36, 1) both;
        }

        .result-card::before {
            content: '';
            position: absolute;
            top: 0; left: 0; right: 0; bottom: 0;
            z-index: -1;
            background: rgba(255, 255, 255, 0.06);
            backdrop-filter: blur(10px) saturate(180%);
            -webkit-backdrop-filter: blur(10px) saturate(180%);
            border-radius: inherit;
            pointer-events: none;
            transition: var(--spring-transition);
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

        .result-card:hover {
            border-color: rgba(255, 255, 255, 0.28);
            box-shadow: 
                inset 0 1px 0 rgba(255, 255, 255, 0.35),
                0 8px 24px rgba(0, 0, 0, 0.08);
            transform: translateY(-1px);
        }

        .result-card:hover::before {
            background: rgba(255, 255, 255, 0.14);
            backdrop-filter: blur(15px) saturate(180%) brightness(1.05);
            -webkit-backdrop-filter: blur(15px) saturate(180%) brightness(1.05);
            filter: url(#glass-distortion-subtle);
        }

        .result-card:active {
            transform: translateY(0);
        }

        /* Search mode rows: clean flat highlights inside glass */
        body.search-mode .result-card {
            border: 1px solid transparent;
            background: transparent;
            box-shadow: none;
            border-radius: 10px;
        }
        body.search-mode .result-card::before {
            display: none !important;
        }
        body.search-mode .result-card:hover {
            background: rgba(255, 255, 255, 0.08) !important;
            border-color: rgba(255, 255, 255, 0.12);
            transform: translateY(0);
            box-shadow: none;
        }

        .result-header {
            display: flex;
            flex-direction: column;
            gap: 0.15rem;
        }

        .filename-link {
            font-size: 15px;
            color: var(--text-primary);
            text-decoration: none;
            font-weight: 600;
            word-break: break-all;
            line-height: 1.3;
            width: fit-content;
            transition: color 0.2s;
        }

        .result-card:hover .filename-link {
            color: var(--primary);
        }

        .file-path-row {
            display: flex;
            align-items: center;
            gap: 0.5rem;
            font-size: 11px;
            color: var(--text-secondary);
            opacity: 0.85;
            margin-top: 0.05rem;
        }

        .file-path-text {
            word-break: break-all;
            font-family: monospace;
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
            opacity: 0.4;
        }

        .result-card:hover .copy-btn {
            opacity: 1;
        }

        .copy-btn:hover {
            color: var(--text-primary);
            background: rgba(255, 255, 255, 0.08);
        }

        .result-text {
            font-size: 13px;
            line-height: 1.5;
            color: #3a3a3c;
            white-space: pre-wrap;
            word-break: break-word;
            margin-top: 0.15rem;
        }

        /* Adjust text color on dark tinted backdrop */
        body.search-mode .result-text {
            color: #d1d1d6;
        }
        body.search-mode .result-card:hover .result-text {
            color: #ffffff;
        }
        body.search-mode .filename-link {
            color: #ffffff;
        }
        body.search-mode .file-path-text {
            color: #aeaeb2;
        }

        .result-meta-row {
            display: flex;
            align-items: center;
            gap: 0.5rem;
            margin-top: 0.2rem;
        }

        .score-text {
            font-size: 10px;
            color: var(--text-secondary);
            font-family: monospace;
            background: rgba(255, 255, 255, 0.06);
            border: 1px solid rgba(255, 255, 255, 0.08);
            padding: 0.1rem 0.35rem;
            border-radius: 4px;
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
            width: 2.2rem;
            height: 2.2rem;
            color: var(--text-secondary);
            opacity: 0.3;
            margin-bottom: 0.25rem;
        }

        .toast {
            position: fixed;
            bottom: 2rem;
            background: rgba(29, 29, 31, 0.85);
            color: white;
            padding: 0.65rem 1.25rem;
            border-radius: 10px;
            font-size: 13px;
            font-weight: 500;
            backdrop-filter: blur(20px);
            -webkit-backdrop-filter: blur(20px);
            box-shadow: 0 8px 30px rgba(0, 0, 0, 0.15);
            pointer-events: none;
            opacity: 0;
            transform: translateY(1rem);
            transition: var(--spring-transition);
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

        /* Loading Indicator Styled as a Subtle Pill */
        .indexing-indicator {
            background: rgba(255, 255, 255, 0.7);
            border: 1px solid rgba(255, 255, 255, 0.2);
            border-radius: 9999px;
            padding: 0.4rem 1rem;
            display: inline-flex;
            align-items: center;
            gap: 0.5rem;
            width: fit-content;
            margin: 0 auto 1rem;
            box-shadow: 0 4px 15px rgba(0,0,0,0.05);
            backdrop-filter: blur(20px) saturate(180%);
            -webkit-backdrop-filter: blur(20px) saturate(180%);
            animation: pulse 2s infinite ease-in-out;
        }

        .indexing-spinner {
            width: 0.85rem;
            height: 0.85rem;
            border: 2px solid rgba(0, 0, 0, 0.05);
            border-top: 2px solid var(--primary);
            border-radius: 50%;
            animation: spin 0.8s linear infinite;
        }

        @keyframes pulse {
            0% { opacity: 0.7; }
            50% { opacity: 1; }
            100% { opacity: 0.7; }
        }

        .indexing-notice {
            display: flex;
            align-items: center;
            justify-content: center;
            color: var(--primary);
            background: rgba(10, 132, 255, 0.05);
            border: 1px solid rgba(10, 132, 255, 0.15);
            border-radius: 12px;
            padding: 1rem;
            margin-top: 1rem;
            text-align: center;
        }

        /* Detail Modal styling */
        .detail-modal {
            position: fixed;
            top: 0;
            left: 0;
            right: 0;
            bottom: 0;
            background: rgba(0, 0, 0, 0.3);
            z-index: 2000;
            display: flex;
            align-items: center;
            justify-content: center;
            backdrop-filter: blur(25px) saturate(180%);
            -webkit-backdrop-filter: blur(25px) saturate(180%);
            animation: fadeIn 0.3s cubic-bezier(0.22, 1, 0.36, 1);
        }

        @keyframes fadeIn {
            from { opacity: 0; }
            to { opacity: 1; }
        }

        .detail-modal-content {
            position: relative;
            background: transparent;
            border: 1px solid rgba(255, 255, 255, 0.18);
            border-radius: 20px;
            padding: 2rem;
            max-width: 650px;
            width: 90%;
            box-shadow: 
                inset 0 1px 0 rgba(255, 255, 255, 0.25),
                0 30px 70px rgba(0,0,0,0.3);
            display: flex;
            flex-direction: column;
            gap: 1.5rem;
            max-height: 85vh;
            overflow: hidden;
            transform: scale(0.95);
            animation: scaleUp 0.4s cubic-bezier(0.22, 1, 0.36, 1) forwards;
        }

        .detail-modal-content::before {
            content: '';
            position: absolute;
            top: 0; left: 0; right: 0; bottom: 0;
            z-index: -1;
            background: rgba(255, 255, 255, 0.10);
            backdrop-filter: blur(40px) saturate(200%) brightness(1.05);
            -webkit-backdrop-filter: blur(40px) saturate(200%) brightness(1.05);
            filter: url(#glass-distortion-subtle);
            border-radius: inherit;
            pointer-events: none;
        }

        @keyframes scaleUp {
            to { transform: scale(1); }
        }

        .detail-modal-header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            border-bottom: 1px solid rgba(255, 255, 255, 0.1);
            padding-bottom: 0.75rem;
        }

        .detail-modal-header h3 {
            font-size: 1.2rem;
            font-weight: 700;
            color: var(--text-primary);
            letter-spacing: -0.015em;
        }

        .close-btn {
            background: rgba(255, 255, 255, 0.08);
            border: none;
            font-size: 1.1rem;
            line-height: 1;
            color: var(--text-secondary);
            cursor: pointer;
            width: 24px;
            height: 24px;
            border-radius: 50%;
            display: flex;
            align-items: center;
            justify-content: center;
            transition: all 0.2s;
        }

        .close-btn:hover {
            background: rgba(255, 255, 255, 0.15);
            color: var(--text-primary);
        }

        .detail-modal-body {
            display: flex;
            flex-direction: column;
            gap: 1.25rem;
            overflow-y: auto;
            max-height: 60vh;
            padding-right: 4px;
        }

        .detail-modal-body::-webkit-scrollbar {
            width: 6px;
        }
        .detail-modal-body::-webkit-scrollbar-track {
            background: transparent;
        }
        .detail-modal-body::-webkit-scrollbar-thumb {
            background: rgba(0, 0, 0, 0.12);
            border-radius: 10px;
        }

        .detail-field {
            display: flex;
            flex-direction: column;
            gap: 0.35rem;
        }

        .detail-label {
            font-size: 10px;
            font-weight: 700;
            color: var(--text-secondary);
            text-transform: uppercase;
            letter-spacing: 0.05em;
        }

        .detail-value {
            font-size: 14px;
            color: var(--text-primary);
            word-break: break-all;
            line-height: 1.4;
        }

        .detail-value.bold {
            font-weight: 600;
            font-size: 16px;
        }

        .detail-value.monospace {
            font-family: monospace;
            background: rgba(255, 255, 255, 0.08);
            padding: 0.2rem 0.5rem;
            border-radius: 6px;
            font-size: 11px;
            border: 1px solid rgba(255,255,255,0.06);
        }

        .detail-content-text {
            font-size: 13px;
            line-height: 1.6;
            color: var(--text-primary);
            white-space: pre-wrap;
            word-break: break-word;
            background: rgba(255, 255, 255, 0.08);
            border: 1px solid rgba(255, 255, 255, 0.06);
            border-radius: 10px;
            padding: 1rem;
            max-height: 300px;
            overflow-y: auto;
        }
        
        .detail-content-text::-webkit-scrollbar {
            width: 6px;
        }
        .detail-content-text::-webkit-scrollbar-track {
            background: transparent;
        }
        .detail-content-text::-webkit-scrollbar-thumb {
            background: rgba(0, 0, 0, 0.12);
            border-radius: 10px;
        }

        .onboard-modal {
            position: fixed;
            top: 0;
            left: 0;
            right: 0;
            bottom: 0;
            background: rgba(245, 245, 247, 0.6);
            z-index: 1000;
            display: flex;
            align-items: center;
            justify-content: center;
            backdrop-filter: blur(30px) saturate(180%);
            -webkit-backdrop-filter: blur(30px) saturate(180%);
        }

        .onboard-content {
            position: relative;
            background: transparent;
            border: 1px solid rgba(255, 255, 255, 0.18);
            border-radius: 24px;
            padding: 2.5rem;
            max-width: 500px;
            width: 90%;
            text-align: center;
            box-shadow: 
                inset 0 1px 0 rgba(255, 255, 255, 0.25),
                0 30px 70px rgba(0,0,0,0.15);
            display: flex;
            flex-direction: column;
            align-items: center;
            gap: 1rem;
            overflow: hidden;
        }

        .onboard-content::before {
            content: '';
            position: absolute;
            top: 0; left: 0; right: 0; bottom: 0;
            z-index: -1;
            background: rgba(255, 255, 255, 0.10);
            backdrop-filter: blur(40px) saturate(180%) brightness(1.05);
            -webkit-backdrop-filter: blur(40px) saturate(180%) brightness(1.05);
            filter: url(#glass-distortion-subtle);
            border-radius: inherit;
            pointer-events: none;
        }

        .onboard-content h2 {
            font-size: 1.8rem;
            font-weight: 700;
            letter-spacing: -0.025em;
            color: var(--text-primary);
            margin-bottom: 0.25rem;
        }

        .setup-banner {
            z-index: 1100;
        }
    </style>
</head>
<body>
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
    <div id="onboard-modal" class="onboard-modal" style="display:none">
        <div class="onboard-content">
            <h2>Chào mừng bạn đến với Better Recoll</h2>
            <p style="color: var(--text-secondary); margin-bottom: 1.5rem; font-size: 0.95rem;">Chọn một thư mục chính chứa tài liệu của bạn để bắt đầu lập chỉ mục nhanh.</p>
            <div style="display: flex; flex-direction: column; gap: 1rem; align-items: center; width: 100%;">
                <button id="onboard-pick-btn" class="setup-btn" style="background: var(--primary); font-size: 1.05rem; padding: 0.6rem 1.5rem;" onclick="triggerOnboardPicker()">Chọn thư mục tài liệu...</button>
                <span id="onboard-path" style="font-family: monospace; color: var(--text-secondary); word-break: break-all; font-size: 0.85rem; text-align: center;">Chưa chọn thư mục nào</span>
                <button id="onboard-start-btn" class="setup-btn" style="background: var(--exact-color); font-size: 1.05rem; padding: 0.6rem 1.5rem; display: none;" onclick="startOnboarding()">Bắt đầu sử dụng</button>
            </div>
            <div id="onboard-status" style="margin-top: 1.5rem; font-size: 0.95rem; text-align: center; color: var(--text-secondary); display: none;"></div>
        </div>
    </div>

    <div class="container">
        <!-- Navigation -->
        <nav class="main-nav">
            <div class="nav-brand">SFS BETTER RECOLL</div>
            <div class="nav-links">
                <button id="nav-search-btn" class="nav-link active" onclick="showView('search')">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><circle cx="11" cy="11" r="8"></circle><line x1="21" y1="21" x2="16.65" y2="16.65"></line></svg>
                    Tìm kiếm
                </button>
                <button id="nav-setting-btn" class="nav-link" onclick="showView('setting')">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><circle cx="12" cy="12" r="3"></circle><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l-.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"></path></svg>
                    Cài đặt
                </button>
            </div>
        </nav>

        <!-- Setup Warning Banner (Visible on both views if missing) -->
        <div id="setup-banner" class="setup-banner">
            <div class="setup-text">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"></path><line x1="12" y1="9" x2="12" y2="13"></line><line x1="12" y1="17" x2="12.01" y2="17"></line></svg>
                <span id="setup-banner-msg">Chưa có model AI — cần tải (khoảng 4GB).</span>
            </div>
            <button id="setup-btn" class="setup-btn" onclick="startSetup()">Tải ngay</button>
        </div>

        <!-- Global Indexing Indicator -->
        <div id="indexing-indicator" class="indexing-indicator" style="display:none">
            <div class="indexing-spinner"></div>
            <span id="indexing-status-text" style="font-size: 0.9rem; color: #a5b4fc;">Đang lập chỉ mục...</span>
        </div>

        <!-- VIEW 1: SEARCH VIEW -->
        <div class="view" id="search-view">

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

            <div class="results-container">
                <div class="results-scroll-area">
                    <!-- CHÍNH XÁC Section -->
                    <div class="results-section exact" id="exact-section" style="display:none">
                        <div class="column-header">
                            <div class="column-title exact">
                                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"></path><polyline points="22 4 12 14.01 9 11.01"></polyline></svg>
                                CHÍNH XÁC
                                <span id="exact-count" class="badge-count" style="display:none">0</span>
                            </div>
                        </div>
                        <div id="exact-list" class="results-list"></div>
                    </div>

                    <!-- GỢI Ý Section -->
                    <div class="results-section suggest" id="suggest-section" style="display:none">
                        <div class="column-header">
                            <div class="column-title suggest">
                                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"></path><line x1="12" y1="9" x2="12" y2="13"></line><line x1="12" y1="17" x2="12.01" y2="17"></line></svg>
                                GỢI Ý
                                <span id="suggest-count" class="badge-count" style="display:none">0</span>
                            </div>
                        </div>
                        <div id="suggest-list" class="results-list"></div>
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
            <div class="results-column" style="min-height: auto;">
                <div class="column-header">
                    <div class="column-title" style="color: var(--primary)">
                        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><circle cx="12" cy="12" r="3"></circle><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l-.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"></path></svg>
                        Lập chỉ mục thư mục
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
                        Thư mục đã lập chỉ mục
                    </div>
                </div>
                
                <div id="folders-list" style="margin-top: 1rem; display: flex; flex-direction: column; gap: 0.75rem;">
                    <div style="color: var(--text-secondary); font-size: 0.9rem; font-style: italic;">Chưa có thư mục nào được lập chỉ mục trong phiên này.</div>
                </div>
            </div>
        </div>
    </div>

    <!-- Detail Modal -->
    <div id="detail-modal" class="detail-modal" style="display:none" onclick="closeDetailModal(event)">
        <div class="detail-modal-content" onclick="event.stopPropagation()">
            <div class="detail-modal-header">
                <h3>Chi tiết tài liệu</h3>
                <button class="close-btn" onclick="hideDetailModal()">&times;</button>
            </div>
            <div class="detail-modal-body">
                <div class="detail-field">
                    <span class="detail-label">Tên file:</span>
                    <span id="detail-filename" class="detail-value bold"></span>
                </div>
                <div class="detail-field">
                    <span class="detail-label">Đường dẫn:</span>
                    <div style="display: flex; gap: 0.5rem; align-items: center;">
                        <span id="detail-filepath" class="detail-value monospace"></span>
                        <button class="copy-btn" id="detail-copy-btn" title="Sao chép đường dẫn">
                            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path></svg>
                        </button>
                    </div>
                </div>
                <div class="detail-field">
                    <span class="detail-label">Điểm số (Score):</span>
                    <span id="detail-score" class="detail-value monospace"></span>
                </div>
                <div class="detail-field">
                    <span class="detail-label">Nội dung đoạn trích:</span>
                    <div id="detail-content" class="detail-content-text"></div>
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
        let isIndexingActive = false;

        // View Toggling
        function showView(viewName) {
            document.querySelectorAll('.view').forEach(v => v.style.display = 'none');
            document.querySelectorAll('.nav-link').forEach(l => l.classList.remove('active'));

            const nav = document.querySelector('.main-nav');
            const header = document.querySelector('header');

            if (viewName === 'search') {
                document.getElementById('search-view').style.display = 'flex';
                document.getElementById('nav-search-btn').classList.add('active');
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
                document.getElementById('nav-setting-btn').classList.add('active');
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
            statusDiv.style.color = 'var(--text-secondary)';

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
                statusDiv.style.color = 'var(--exact-color)';
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
            resultsMessage.className = 'empty-state';
            resultsMessage.style.color = '';
            resultsMessage.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></svg><div>Chưa có tìm kiếm</div>';

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
            resultsMessage.className = 'empty-state';
            resultsMessage.style.color = '#ef4444';
            resultsMessage.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></svg><div>' + displayMsg + '</div>';

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

            resultsMessage.style.color = '';

            const resultsContainer = document.querySelector('.results-container');
            if (resultsContainer) {
                resultsContainer.style.display = 'flex';
            }

            if (totalResults === 0) {
                exactSection.style.display = 'none';
                suggestSection.style.display = 'none';
                resultsMessage.style.display = 'flex';
                resultsMessage.className = 'empty-state';
                if (isIndexingActive) {
                    resultsMessage.innerHTML = '<div style="color: var(--primary); font-weight: 500;">⏳ Đang lập chỉ mục thêm tài liệu, kết quả sẽ đầy đủ hơn sau...</div>';
                } else {
                    resultsMessage.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></svg><div>Không tìm thấy kết quả</div>';
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
                    resultsMessage.className = 'indexing-notice';
                    resultsMessage.innerHTML = '<div style="color: var(--primary); font-weight: 500; text-align: center; width: 100%;">⏳ Đang lập chỉ mục thêm tài liệu, kết quả sẽ đầy đủ hơn sau...</div>';
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

            return '<div class="result-card" style="animation-delay: ' + delay + 'ms" onclick="showDetailFromJSON(this.querySelector(\'.filename-link\').getAttribute(\'data-json\'))">' +
                   '  <div class="result-header">' +
                   '    <a class="filename-link" onclick="event.stopPropagation(); showDetailFromJSON(this.getAttribute(\'data-json\'))" data-json="' + jsonStr + '" title="Xem chi tiết">' + filename + '</a>' +
                   '    <div class="file-path-row">' +
                   '      <span class="file-path-text" title="' + escapedPath + '">' + escapedPath + '</span>' +
                   '      <button class="copy-btn" onclick="event.stopPropagation(); copyToClipboard(this.getAttribute(\'data-path\'))" data-path="' + escapedPath + '" title="Sao chép đường dẫn">' +
                   '        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path></svg>' +
                   '      </button>' +
                   '    </div>' +
                   '  </div>' +
                   '  <div class="result-text">' + escapedText + '</div>' +
                   '  <div class="result-meta-row">' +
                   '    <span class="score-text">Score: ' + displayScore + '</span>' +
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

            document.getElementById('detail-modal').style.display = 'flex';
        }

        function hideDetailModal() {
            document.getElementById('detail-modal').style.display = 'none';
        }

        function closeDetailModal(event) {
            if (event.target.id === 'detail-modal') {
                hideDetailModal();
            }
        }

        function showDetailFromJSON(jsonStr) {
            const data = JSON.parse(jsonStr);
            showDetail(data.filename, data.filepath, data.score, data.text);
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
                    if (!status.indexing) {
                        searchInput.disabled = false;
                        searchInput.placeholder = "Nhập từ khóa tìm kiếm...";
                    } else {
                        searchInput.disabled = false;
                        searchInput.placeholder = "Nhập từ khóa tìm kiếm...";
                    }

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
                    onboardModal.style.display = 'flex';
                    if (status.indexing) {
                        onboardPickBtn.style.display = 'none';
                        onboardStartBtn.style.display = 'none';
                        onboardStatus.style.display = 'block';
                        onboardStatus.textContent = 'Đang thiết lập ban đầu (nhanh): ' + status.currentDir + ' (' + status.filesIndexed + ' file)...';
                    }
                } else {
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
                        indexStatusDiv.style.color = 'var(--text-secondary)';
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
                        indexStatusDiv.style.color = 'var(--exact-color)';
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

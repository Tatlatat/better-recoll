package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"sfs/internal/chunk"
	"sfs/internal/engine"
	"sfs/internal/model"
	"sfs/internal/reader"
)

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  sfs setup [--light] Download required model files")
	fmt.Println("  sfs index <dir>      Index a directory")
	fmt.Println("  sfs search <query>  Search for a query")
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "index":
		if len(os.Args) < 3 {
			fmt.Println("Error: missing directory path")
			printUsage()
			os.Exit(1)
		}
		dir := os.Args[2]

		// Get root path from environment variable or default to CWD
		root := os.Getenv("SFS_ROOT")
		if root == "" {
			var err error
			root, err = os.Getwd()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error getting current working directory: %v\n", err)
				os.Exit(1)
			}
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error determining absolute root path: %v\n", err)
			os.Exit(1)
		}

		// Count the files and chunks that will be indexed
		var fileCount, chunkCount int
		err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
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
				// Bỏ qua file đọc lỗi (PDF hỏng, mã hoá lạ) — không làm sập việc đếm.
				return nil
			}
			fileCount++
			chunks := chunk.Chunk(text, 512)
			chunkCount += len(chunks)
			return nil
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error scanning directory: %v\n", err)
			os.Exit(1)
		}

		// Initialize Engine
		eng, err := engine.New(engine.DefaultConfig(absRoot, filepath.Join(absRoot, ".sfsindex")))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing engine: %v\n", err)
			os.Exit(1)
		}
		defer eng.Close()

		// Perform Indexing
		if err := eng.Index(dir); err != nil {
			fmt.Fprintf(os.Stderr, "Error during indexing: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Indexed %d files (%d chunks).\n", fileCount, chunkCount)

	case "search":
		if len(os.Args) < 3 {
			fmt.Println("Error: missing search query")
			printUsage()
			os.Exit(1)
		}
		query := os.Args[2]

		// Get root path from environment variable or default to CWD
		root := os.Getenv("SFS_ROOT")
		if root == "" {
			var err error
			root, err = os.Getwd()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error getting current working directory: %v\n", err)
				os.Exit(1)
			}
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error determining absolute root path: %v\n", err)
			os.Exit(1)
		}

		// Initialize Engine
		eng, err := engine.New(engine.DefaultConfig(absRoot, filepath.Join(absRoot, ".sfsindex")))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing engine: %v\n", err)
			os.Exit(1)
		}
		defer eng.Close()

		// Search
		results, err := eng.SearchRanked(query, 10)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error searching: %v\n", err)
			os.Exit(1)
		}

		printBucket := func(header string, bucket []engine.Result) {
			fmt.Println(header)
			if len(bucket) == 0 {
				fmt.Println("(không có)")
				return
			}
			for _, r := range bucket {
				runes := []rune(r.Text)
				snippet := string(runes)
				if len(runes) > 80 {
					snippet = string(runes[:80]) + "..."
				}
				// Sanitize newlines to display neatly in terminal list
				snippet = strings.ReplaceAll(snippet, "\n", " ")
				fmt.Printf("%f  %s\n", r.Score, r.FilePath)
				fmt.Printf("%s\n\n", snippet)
			}
		}

		printBucket("CHÍNH XÁC:", results.Exact)
		printBucket("GỢI Ý:", results.Suggest)

	case "setup":
		light := false
		if len(os.Args) > 2 {
			for _, arg := range os.Args[2:] {
				if arg == "--light" {
					light = true
				} else if arg == "--help" || arg == "-h" {
					fmt.Println("Usage: sfs setup [--light]")
					fmt.Println("Downloads the required BGE-M3 and BGE-Reranker ONNX models and configuration files.")
					fmt.Println("Options:")
					fmt.Println("  --light  Download the int8 quantized reranker model instead of the full model")
					os.Exit(0)
				} else {
					fmt.Printf("Error: unknown argument %q\n", arg)
					fmt.Println("Usage: sfs setup [--light]")
					os.Exit(1)
				}
			}
		}
		runSetup(light)

	default:
		fmt.Printf("Error: unknown subcommand %q\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

type progressWriter struct {
	filename           string
	totalBytes         int64
	written            int64
	lastPrintedPercent int
	lastPrintedMB      int64
	writer             io.Writer
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.writer.Write(p)
	if err != nil {
		return n, err
	}
	pw.written += int64(n)
	currentMB := pw.written / (1024 * 1024)

	if pw.totalBytes > 0 {
		totalMB := pw.totalBytes / (1024 * 1024)
		percent := int(pw.written * 100 / pw.totalBytes)
		if percent >= pw.lastPrintedPercent+1 || currentMB >= pw.lastPrintedMB+10 {
			pw.lastPrintedPercent = percent
			pw.lastPrintedMB = currentMB
			fmt.Printf("\rDownloading %s: %d / %d MB (%d%%)", pw.filename, currentMB, totalMB, percent)
			os.Stdout.Sync()
		}
	} else {
		if currentMB >= pw.lastPrintedMB+10 {
			pw.lastPrintedMB = currentMB
			fmt.Printf("\rDownloading %s: %d MB...", pw.filename, currentMB)
			os.Stdout.Sync()
		}
	}
	return n, nil
}

type HTTPError struct {
	StatusCode int
	Status     string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("bad status: %s", e.Status)
}

func downloadFile(url string, destPath string) error {
	filename := filepath.Base(destPath)

	// Try HEAD request first to check size
	var expectedSize int64 = -1
	req, err := http.NewRequest("HEAD", url, nil)
	if err == nil {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				expectedSize = resp.ContentLength
			} else if resp.StatusCode == http.StatusNotFound {
				return &HTTPError{StatusCode: resp.StatusCode, Status: resp.Status}
			}
		}
	}

	if expectedSize > 0 {
		if info, err := os.Stat(destPath); err == nil && info.Size() == expectedSize {
			fmt.Printf("File %s already exists with correct size (%d MB). Skipping.\n", filename, expectedSize/(1024*1024))
			return nil
		}
	}

	// Send GET request to download
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &HTTPError{StatusCode: resp.StatusCode, Status: resp.Status}
	}

	if resp.ContentLength > 0 {
		expectedSize = resp.ContentLength
	}

	// Double check if file exists with correct size from GET response
	if expectedSize > 0 {
		if info, err := os.Stat(destPath); err == nil && info.Size() == expectedSize {
			fmt.Printf("File %s already exists with correct size (%d MB). Skipping.\n", filename, expectedSize/(1024*1024))
			return nil
		}
	}

	// Create output file
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	pw := &progressWriter{
		filename:           filename,
		totalBytes:         expectedSize,
		writer:             out,
		lastPrintedPercent: -1,
		lastPrintedMB:      -10,
	}

	fmt.Printf("Downloading %s...", filename)
	os.Stdout.Sync()

	_, err = io.Copy(pw, resp.Body)
	if err != nil {
		return err
	}

	finalMB := pw.written / (1024 * 1024)
	fmt.Printf("\rDownloading %s: Completed (%d MB).\n", filename, finalMB)
	return nil
}

func runSetup(light bool) {
	root := model.DefaultModelRoot()
	if root == "." {
		if homeDir, err := os.UserHomeDir(); err == nil {
			root = filepath.Join(homeDir, ".sfs")
		} else if execPath, err := os.Executable(); err == nil {
			root = filepath.Dir(execPath)
		}
	}

	bgeM3Dir := filepath.Join(root, "models", "onnx", "bge-m3")
	bgeRerankerDir := filepath.Join(root, "models", "onnx", "bge-reranker")

	fmt.Printf("Setting up models directory at: %s\n", filepath.Join(root, "models"))

	// Create directories
	if err := os.MkdirAll(bgeM3Dir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating bge-m3 directory: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(bgeRerankerDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating bge-reranker directory: %v\n", err)
		os.Exit(1)
	}

	// 1. Download BGE-M3 (always full model)
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

	fmt.Println("\n--- Downloading BGE-M3 ---")
	for _, file := range bgeM3Files {
		url := fmt.Sprintf("%s%s?download=true", bgeM3BaseURL, file.srcPath)
		destPath := filepath.Join(bgeM3Dir, file.destName)

		if err := downloadFile(url, destPath); err != nil {
			if httpErr, ok := err.(*HTTPError); ok && httpErr.StatusCode == http.StatusNotFound && file.destName == "special_tokens_map.json" {
				fmt.Printf("\nSkipping optional file %s (not found).\n", file.destName)
				continue
			}
			fmt.Fprintf(os.Stderr, "\nError downloading %s: %v\n", file.destName, err)
			os.Exit(1)
		}
	}

	fmt.Println("\nSetup for BAAI/bge-m3 completed successfully.")

	// 2. Download BGE-Reranker (optionally light)
	var rerankerFiles []struct {
		srcPath  string
		destName string
	}
	if light {
		rerankerFiles = []struct {
			srcPath  string
			destName string
		}{
			{srcPath: "onnx/model_int8.onnx", destName: "model.onnx"},
			{srcPath: "tokenizer.json", destName: "tokenizer.json"},
			{srcPath: "tokenizer_config.json", destName: "tokenizer_config.json"},
			{srcPath: "special_tokens_map.json", destName: "special_tokens_map.json"},
			{srcPath: "config.json", destName: "config.json"},
		}
	} else {
		rerankerFiles = []struct {
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
	}

	bgeRerankerBaseURL := "https://huggingface.co/onnx-community/bge-reranker-v2-m3-ONNX/resolve/main/"

	fmt.Println("\n--- Downloading BGE-Reranker ---")
	for _, file := range rerankerFiles {
		url := fmt.Sprintf("%s%s?download=true", bgeRerankerBaseURL, file.srcPath)
		destPath := filepath.Join(bgeRerankerDir, file.destName)

		if err := downloadFile(url, destPath); err != nil {
			if httpErr, ok := err.(*HTTPError); ok && httpErr.StatusCode == http.StatusNotFound && file.destName == "special_tokens_map.json" {
				fmt.Printf("\nSkipping optional file %s (not found).\n", file.destName)
				continue
			}
			fmt.Fprintf(os.Stderr, "\nError downloading %s: %v\n", file.destName, err)
			os.Exit(1)
		}
	}

	fmt.Println("\nSetup for onnx-community/bge-reranker-v2-m3-ONNX completed successfully.")
}

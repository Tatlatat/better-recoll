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
	fmt.Println("  sfs setup           Download required model files")
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
				return err
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
		if len(os.Args) > 2 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
			fmt.Println("Usage: sfs setup")
			fmt.Println("Downloads the required BGE-M3 ONNX models and configuration files.")
			os.Exit(0)
		}
		runSetup()

	default:
		fmt.Printf("Error: unknown subcommand %q\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

type progressWriter struct {
	filename    string
	totalBytes  int64
	written     int64
	lastPrinted int64
	writer      io.Writer
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.writer.Write(p)
	if err != nil {
		return n, err
	}
	pw.written += int64(n)
	currentMB := pw.written / (1024 * 1024)
	if currentMB > pw.lastPrinted {
		pw.lastPrinted = currentMB
		if pw.totalBytes > 0 {
			totalMB := pw.totalBytes / (1024 * 1024)
			fmt.Printf("\rDownloading %s: %d MB / %d MB...", pw.filename, currentMB, totalMB)
		} else {
			fmt.Printf("\rDownloading %s: %d MB...", pw.filename, currentMB)
		}
		os.Stdout.Sync()
	}
	return n, nil
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
		return fmt.Errorf("bad status: %s", resp.Status)
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
		filename:   filename,
		totalBytes: expectedSize,
		writer:     out,
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

func runSetup() {
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

	files := []struct {
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

	baseURL := "https://huggingface.co/BAAI/bge-m3/resolve/main/"

	for _, file := range files {
		url := fmt.Sprintf("%s%s?download=true", baseURL, file.srcPath)
		destPath := filepath.Join(bgeM3Dir, file.destName)

		if err := downloadFile(url, destPath); err != nil {
			fmt.Fprintf(os.Stderr, "\nError downloading %s: %v\n", file.destName, err)
			os.Exit(1)
		}
	}

	fmt.Println("\nSetup for BAAI/bge-m3 completed successfully.")
	fmt.Println("\nNote: BAAI/bge-reranker is not pre-exported to ONNX on Hugging Face.")
	fmt.Println("To use the reranker, you must export it separately using optimum-cli:")
	fmt.Println("  pip install optimum[onnxruntime]")
	fmt.Println("  optimum-cli export onnx --model BAAI/bge-reranker-large --task text-classification <dest_dir>")
	fmt.Printf("Where <dest_dir> should be: %s\n", bgeRerankerDir)
}

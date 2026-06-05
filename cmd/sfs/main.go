package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sfs/internal/chunk"
	"sfs/internal/engine"
	"sfs/internal/reader"
)

func printUsage() {
	fmt.Println("Usage:")
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
		results, err := eng.Search(query, 10)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error searching: %v\n", err)
			os.Exit(1)
		}

		if len(results) == 0 {
			fmt.Println("No results found.")
			return
		}

		for _, r := range results {
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

	default:
		fmt.Printf("Error: unknown subcommand %q\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

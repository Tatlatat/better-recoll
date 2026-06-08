package main

import (
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"sfs/internal/engine"
	"sfs/internal/webui"
)

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

	srv, err := webui.Start(cfg, port)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
	defer srv.Shutdown()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	<-stop
	log.Println("Shutting down server gracefully...")
}

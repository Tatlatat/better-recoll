package model

import (
	"os"
	"path/filepath"
)

// ResolveRuntimePaths finds the onnxruntime shared library and model root directory at runtime.
func ResolveRuntimePaths() (string, string) {
	return findOnnxRuntimeLib(), DefaultModelRoot()
}

// findOnnxRuntimeLib searches for the onnxruntime shared library in a set of standard locations and returns the first existing path.
func findOnnxRuntimeLib() string {
	// (1) env var SFS_ONNXRUNTIME_LIB if set
	if envLib := os.Getenv("SFS_ONNXRUNTIME_LIB"); envLib != "" {
		if info, err := os.Stat(envLib); err == nil && !info.IsDir() {
			return envLib
		}
	}

	// (2) a libs/ or lib/ directory next to the executable
	if execPath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(execPath)
		subdirs := []string{"libs", "lib"}
		filenames := []string{"libonnxruntime.dylib", "libonnxruntime.so", "onnxruntime.dll"}
		for _, subdir := range subdirs {
			for _, name := range filenames {
				p := filepath.Join(execDir, subdir, name)
				if info, err := os.Stat(p); err == nil && !info.IsDir() {
					return p
				}
			}
		}
	}

	// (3) /opt/homebrew/lib/libonnxruntime.dylib (mac brew fallback)
	p3 := "/opt/homebrew/lib/libonnxruntime.dylib"
	if info, err := os.Stat(p3); err == nil && !info.IsDir() {
		return p3
	}

	// (4) /usr/local/lib/libonnxruntime.dylib
	p4 := "/usr/local/lib/libonnxruntime.dylib"
	if info, err := os.Stat(p4); err == nil && !info.IsDir() {
		return p4
	}

	// (5) /usr/lib/x86_64-linux-gnu/libonnxruntime.so (linux)
	p5 := "/usr/lib/x86_64-linux-gnu/libonnxruntime.so"
	if info, err := os.Stat(p5); err == nil && !info.IsDir() {
		return p5
	}

	return ""
}

// DefaultModelRoot returns the models root by searching standard locations in order.
func DefaultModelRoot() string {
	// (1) env SFS_MODELS_DIR
	if dir := os.Getenv("SFS_MODELS_DIR"); dir != "" {
		if info, err := os.Stat(filepath.Join(dir, "models")); err == nil && info.IsDir() {
			return dir
		}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return filepath.Dir(dir)
		}
	}

	// (2) a "models" dir next to the executable
	if execPath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(execPath)
		modelsDir := filepath.Join(execDir, "models")
		if info, err := os.Stat(modelsDir); err == nil && info.IsDir() {
			return execDir
		}
	}

	// (3) ~/.sfs/models (user home)
	if homeDir, err := os.UserHomeDir(); err == nil {
		modelsDir := filepath.Join(homeDir, ".sfs", "models")
		if info, err := os.Stat(modelsDir); err == nil && info.IsDir() {
			return filepath.Join(homeDir, ".sfs")
		}
	}

	// (4) ./models (cwd)
	if info, err := os.Stat("models"); err == nil && info.IsDir() {
		return "."
	}

	return "."
}

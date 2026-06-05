package model

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindOnnxRuntimeLib(t *testing.T) {
	// 1. Test SFS_ONNXRUNTIME_LIB env var override.
	tmpDir := t.TempDir()
	dummyLib := filepath.Join(tmpDir, "libdummy.so")
	err := os.WriteFile(dummyLib, []byte("dummy content"), 0644)
	if err != nil {
		t.Fatalf("failed to write dummy lib: %v", err)
	}

	t.Setenv("SFS_ONNXRUNTIME_LIB", dummyLib)
	resolved := findOnnxRuntimeLib()
	if resolved != dummyLib {
		t.Errorf("expected findOnnxRuntimeLib to return %s, got %s", dummyLib, resolved)
	}

	// 2. Test fallback behaviour when SFS_ONNXRUNTIME_LIB does not exist.
	t.Setenv("SFS_ONNXRUNTIME_LIB", filepath.Join(tmpDir, "non_existent_file.so"))
	// Should not return the non-existent file path
	resolved = findOnnxRuntimeLib()
	if resolved == filepath.Join(tmpDir, "non_existent_file.so") {
		t.Errorf("findOnnxRuntimeLib returned non-existent env-var override")
	}
}

func TestDefaultModelRoot(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Test when SFS_MODELS_DIR points to a directory containing "models"
	modelsDir := filepath.Join(tmpDir, "models")
	err := os.Mkdir(modelsDir, 0755)
	if err != nil {
		t.Fatalf("failed to create models dir: %v", err)
	}

	t.Setenv("SFS_MODELS_DIR", tmpDir)
	root := DefaultModelRoot()
	if root != tmpDir {
		t.Errorf("expected DefaultModelRoot to return %s when SFS_MODELS_DIR points to parent of models, got %s", tmpDir, root)
	}

	// 2. Test when SFS_MODELS_DIR points directly to "models"
	t.Setenv("SFS_MODELS_DIR", modelsDir)
	root = DefaultModelRoot()
	if root != tmpDir {
		t.Errorf("expected DefaultModelRoot to return parent directory %s when SFS_MODELS_DIR points directly to models directory, got %s", tmpDir, root)
	}
}

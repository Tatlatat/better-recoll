package webui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Helper: tạo cây model giả với kích thước cho trước (sparse file, không tốn đĩa).
func writeFakeModel(t *testing.T, root string, sizes map[string]int64) {
	t.Helper()
	for _, rf := range requiredModelFiles {
		sz, ok := sizes[rf.relPath]
		if !ok {
			continue // bỏ qua = file thiếu
		}
		p := filepath.Join(root, rf.relPath)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		if sz > 0 {
			// sparse: nhảy tới sz-1 rồi ghi 1 byte → file báo size sz mà không ghi GB thật
			if _, err := f.Seek(sz-1, 0); err != nil {
				t.Fatal(err)
			}
			if _, err := f.Write([]byte{0}); err != nil {
				t.Fatal(err)
			}
		}
		f.Close()
	}
}

func fullSizes() map[string]int64 {
	m := map[string]int64{}
	for _, rf := range requiredModelFiles {
		m[rf.relPath] = rf.minSize + 1 // vừa đủ qua ngưỡng
	}
	return m
}

// Model đầy đủ → integrity sạch, checkModelExists = true.
func TestVerifyIntegrityComplete(t *testing.T) {
	root := t.TempDir()
	writeFakeModel(t, root, fullSizes())
	bad := verifyModelIntegrity(root)
	if len(bad) != 0 {
		t.Fatalf("model đủ mà báo hỏng: %v", bad)
	}
}

// Thiếu tokenizer.json (đúng lỗi ~/.sfs thật) → phải bị bắt.
func TestVerifyIntegrityMissingTokenizer(t *testing.T) {
	root := t.TempDir()
	sizes := fullSizes()
	delete(sizes, "models/onnx/bge-m3/tokenizer.json")
	writeFakeModel(t, root, sizes)
	bad := verifyModelIntegrity(root)
	if len(bad) == 0 {
		t.Fatal("thiếu tokenizer.json mà KHÔNG bị bắt")
	}
	joined := strings.Join(bad, " ")
	if !strings.Contains(joined, "tokenizer.json") {
		t.Fatalf("báo lỗi không nhắc tokenizer.json: %v", bad)
	}
}

// cleanCorruptModelFiles: dọn file cụt + .part, GIỮ file đủ.
func TestCleanCorruptModelFiles(t *testing.T) {
	root := t.TempDir()
	sizes := fullSizes()
	// làm model.onnx_data cụt + tạo 1 file .part rác
	sizes["models/onnx/bge-m3/model.onnx_data"] = 1000 // cụt
	writeFakeModel(t, root, sizes)
	partPath := filepath.Join(root, "models/onnx/bge-m3/tokenizer.json.part")
	os.WriteFile(partPath, []byte("dở dang"), 0o644)

	n := cleanCorruptModelFiles(root)
	if n < 1 {
		t.Fatalf("phải dọn ít nhất file cụt, dọn %d", n)
	}
	// file cụt phải bị xóa
	if _, err := os.Stat(filepath.Join(root, "models/onnx/bge-m3/model.onnx_data")); !os.IsNotExist(err) {
		t.Error("file cụt model.onnx_data chưa bị dọn")
	}
	// .part phải bị xóa
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Error(".part rác chưa bị dọn")
	}
	// file ĐỦ (model.onnx) phải còn
	if _, err := os.Stat(filepath.Join(root, "models/onnx/bge-m3/model.onnx")); err != nil {
		t.Error("file đủ model.onnx bị xóa nhầm")
	}
}

// model.onnx_data bị cụt (0-byte / 104MB thay vì 2.3GB) → phải bị bắt.
func TestVerifyIntegrityTruncatedData(t *testing.T) {
	root := t.TempDir()
	sizes := fullSizes()
	sizes["models/onnx/bge-m3/model.onnx_data"] = 104 * 1024 * 1024 // cụt như lỗi thật
	writeFakeModel(t, root, sizes)
	bad := verifyModelIntegrity(root)
	if len(bad) == 0 {
		t.Fatal("model.onnx_data cụt mà KHÔNG bị bắt")
	}
	if !strings.Contains(strings.Join(bad, " "), "model.onnx_data") {
		t.Fatalf("báo lỗi không nhắc model.onnx_data: %v", bad)
	}
}

package reader

import (
	"testing"
)

func TestRegistry(t *testing.T) {
	// Verify all readers are registered
	expectedExts := []string{".pdf", ".docx", ".xlsx"}
	for _, ext := range expectedExts {
		if _, ok := Registry[ext]; !ok {
			t.Errorf("expected extension %s to be registered", ext)
		}
	}

	// Verify ReadFile returns error for unsupported extension
	_, err := ReadFile("test.txt")
	if err == nil {
		t.Error("expected error for unsupported extension .txt, got nil")
	}
}

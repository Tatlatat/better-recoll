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

	// Verify ReadFile returns error for a genuinely unsupported extension.
	// (.txt IS supported now — it's registered by TxtReader's init.)
	_, err := ReadFile("test.unknownext")
	if err == nil {
		t.Error("expected error for unsupported extension .unknownext, got nil")
	}
}

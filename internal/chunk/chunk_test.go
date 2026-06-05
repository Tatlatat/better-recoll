package chunk

import (
	"strings"
	"testing"
)

// Oracle Claude. Chunk cắt text thành đoạn ~size ký tự, giữ Offset tăng dần,
// không mất nội dung, không tạo đoạn rỗng.
func TestChunk(t *testing.T) {
	text := strings.Repeat("Đây là một câu tiếng Việt có dấu. ", 60) // ~2000 ký tự
	chunks := Chunk(text, 512)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for ~2000 chars, got %d", len(chunks))
	}
	lastOffset := -1
	for i, c := range chunks {
		if strings.TrimSpace(c.Text) == "" {
			t.Errorf("chunk %d rỗng", i)
		}
		if c.Offset <= lastOffset {
			t.Errorf("chunk %d offset %d không tăng (prev %d)", i, c.Offset, lastOffset)
		}
		lastOffset = c.Offset
	}
}

func TestChunkShort(t *testing.T) {
	chunks := Chunk("một đoạn ngắn", 512)
	if len(chunks) != 1 {
		t.Fatalf("short text -> 1 chunk, got %d", len(chunks))
	}
}

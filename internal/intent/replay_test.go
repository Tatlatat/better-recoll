package intent

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
)

func TestOfflineReplay(t *testing.T) {
	// Parse events from .sfsindex/events.jsonl if it exists
	f, err := os.Open("../../.sfsindex/events.jsonl")
	if err != nil {
		t.Skip("No events.jsonl found, skipping replay test")
	}
	defer f.Close()

	var events []Event
	paths := make(map[string]bool)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var e Event
		if err := json.Unmarshal(scanner.Bytes(), &e); err == nil {
			events = append(events, e)
			if e.Path != "" {
				paths[e.Path] = true
			}
		}
	}

	// Mock file candidates
	var files []FileCandidate
	for path := range paths {
		files = append(files, FileCandidate{
			Path:    path,
			Vector:  []float32{}, // Mock empty vector
			ModTime: 0,           // Mock mod time
		})
	}

	hitRate, mrr := RunReplay(events, files)
	t.Logf("Offline Replay Results (Offline events: %d):", len(events))
	t.Logf("HitRate@5: %.2f%%", hitRate*100)
	t.Logf("MRR@5:     %.4f", mrr)
}

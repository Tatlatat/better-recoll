package intent

import (
	"testing"
	"time"
)

// File vừa sửa (recency cao) phải xếp trên file cũ, khi profile trống (cold start).
func TestPredictRecencyWins(t *testing.T) {
	now := time.Now()
	files := []FileCandidate{
		{Path: "/cũ.txt", Vector: []float32{1, 0}, ModTime: now.Add(-100 * time.Hour).Unix()},
		{Path: "/mới.txt", Vector: []float32{1, 0}, ModTime: now.Add(-1 * time.Hour).Unix()},
	}
	prof := Profile{FileStats: map[string]*FileStat{}} // cold start
	got := Predict(files, prof, now, 2)
	if len(got) != 2 || got[0].Path != "/mới.txt" {
		t.Fatalf("file mới phải top-1, được: %+v", got)
	}
}

// Khi có interest vector, file khớp ngữ nghĩa được cộng điểm.
func TestPredictCosineHelps(t *testing.T) {
	now := time.Now()
	mt := now.Add(-50 * time.Hour).Unix() // cùng recency
	files := []FileCandidate{
		{Path: "/khớp.txt", Vector: []float32{1, 0}, ModTime: mt},
		{Path: "/lệch.txt", Vector: []float32{0, 1}, ModTime: mt},
	}
	prof := Profile{
		InterestVector: []float32{1, 0}, // quan tâm hướng [1,0]
		FileStats:      map[string]*FileStat{},
	}
	got := Predict(files, prof, now, 2)
	if got[0].Path != "/khớp.txt" {
		t.Fatalf("file khớp ngữ nghĩa phải top-1, được: %+v", got)
	}
}

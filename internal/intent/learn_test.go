package intent

import (
	"math"
	"testing"
	"time"
)

func TestLearn_UpdateWeights(t *testing.T) {
	now := time.Now()

	// Pos file: recency cực kỳ cao (vừa sửa tức thì)
	posFile := FileCandidate{
		Path:    "/pos.txt",
		Vector:  []float32{0, 0}, // no cosine
		ModTime: now.Unix(),      // age = 0 -> sRec = 1.0
	}

	// Neg file: cosine cao, nhưng recency thấp
	negFile := FileCandidate{
		Path:    "/neg.txt",
		Vector:  []float32{1, 0},             // match interest
		ModTime: now.Add(-100 * time.Hour).Unix(), // sRec ~ 0
	}

	prof := Profile{
		InterestVector: []float32{1, 0}, // interest khớp với neg
		FileStats:      make(map[string]*FileStat),
	}

	// Trọng số mặc định: Cos=0.2, Rec=0.6, Freq=0.1, Time=0.1
	// Điểm posScore = w.Rec*1.0 = 0.6
	// Điểm negScore = w.Cos*1.0 = 0.2
	// Để mô phỏng "thuật toán đang sai", ta ép trọng số ban đầu ngược lại
	wInitial := Weights{
		Cos:  0.8,
		Rec:  0.1,
		Freq: 0.05,
		Time: 0.05,
	}

	// check sum before
	sumBefore := wInitial.Cos + wInitial.Rec + wInitial.Freq + wInitial.Time
	if math.Abs(sumBefore-1.0) > 1e-6 {
		t.Fatalf("sumBefore != 1")
	}

	wFinal := Learn(posFile, []FileCandidate{negFile}, prof, wInitial, now)

	// wRec phải TĂNG, wCos phải GIẢM
	if wFinal.Rec <= wInitial.Rec {
		t.Fatalf("wRec phải tăng, got %v <= %v", wFinal.Rec, wInitial.Rec)
	}
	if wFinal.Cos >= wInitial.Cos {
		t.Fatalf("wCos phải giảm, got %v >= %v", wFinal.Cos, wInitial.Cos)
	}

	// Tổng trọng số phải ≈ 1
	sumFinal := wFinal.Cos + wFinal.Rec + wFinal.Freq + wFinal.Time
	if math.Abs(sumFinal-1.0) > 1e-6 {
		t.Fatalf("Tổng trọng số sau update phải ≈ 1, got %v", sumFinal)
	}
}

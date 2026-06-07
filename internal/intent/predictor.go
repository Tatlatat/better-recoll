package intent

import (
	"math"
	"sort"
	"time"
)

// FileCandidate là 1 file ứng viên để xếp hạng (từ store.FileEntries).
type FileCandidate struct {
	Path    string
	Vector  []float32
	ModTime int64 // Unix giây
}

// Prediction là 1 file được dự đoán + điểm + lý do ngắn.
type Prediction struct {
	Path   string  `json:"path"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

// Trọng số cố định cho GĐ2 (xem SCORING_DESIGN; w_time gộp vào sau).
const (
	wCos      = 0.20
	wRec      = 0.60
	wFreq     = 0.20
	recHalfLifeHours = 24.0 // recency half-life 24h
	freqK            = 5.0  // BM25 saturation
)

// Predict xếp hạng files theo công thức toán thuần, trả top-k.
// score = wCos·cosine(interest, file) + wRec·recency + wFreq·frequency.
func Predict(files []FileCandidate, prof Profile, now time.Time, k int) []Prediction {
	if k <= 0 {
		return nil
	}
	lambda := math.Ln2 / recHalfLifeHours
	preds := make([]Prediction, 0, len(files))

	for _, f := range files {
		// recency: exp decay theo max(mtime, lastOpened)
		ref := f.ModTime
		if st := prof.FileStats[f.Path]; st != nil && !st.LastOpened.IsZero() {
			if lo := st.LastOpened.Unix(); lo > ref {
				ref = lo
			}
		}
		ageH := now.Sub(time.Unix(ref, 0)).Hours()
		if ageH < 0 {
			ageH = 0
		}
		sRec := math.Exp(-lambda * ageH)

		// frequency: BM25 saturation count/(count+k)
		var sFreq float64
		if st := prof.FileStats[f.Path]; st != nil {
			c := float64(st.OpenCount)
			sFreq = c / (c + freqK)
		}

		// cosine: dot product (vector đã ~chuẩn hoá); max(0,·)
		var sCos float64
		if len(prof.InterestVector) > 0 {
			var dot float64
			for i := range f.Vector {
				if i < len(prof.InterestVector) {
					dot += float64(f.Vector[i]) * float64(prof.InterestVector[i])
				}
			}
			if dot > 0 {
				sCos = dot
			}
		}

		score := wCos*sCos + wRec*sRec + wFreq*sFreq
		preds = append(preds, Prediction{
			Path:   f.Path,
			Score:  score,
			Reason: reasonFor(sRec, sFreq, sCos),
		})
	}

	sort.SliceStable(preds, func(i, j int) bool { return preds[i].Score > preds[j].Score })
	if len(preds) > k {
		preds = preds[:k]
	}
	return preds
}

// reasonFor trả lý do ngắn dựa yếu tố trội nhất.
func reasonFor(rec, freq, cos float64) string {
	if rec >= freq && rec >= cos {
		return "vừa truy cập gần đây"
	}
	if freq >= cos {
		return "bạn hay mở file này"
	}
	return "liên quan việc bạn đang làm"
}

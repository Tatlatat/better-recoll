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
	recHalfLifeHours = 24.0 // recency half-life 24h
	freqK            = 5.0  // BM25 saturation
	timeSigma        = 2.0
)

// Predict xếp hạng files theo công thức toán thuần, trả top-k với DefaultWeights.
func Predict(files []FileCandidate, prof Profile, now time.Time, k int) []Prediction {
	return PredictWithWeights(files, prof, DefaultWeights(), now, k)
}

// PredictWithWeights cho phép chỉ định trọng số thay vì dùng DefaultWeights.
func PredictWithWeights(files []FileCandidate, prof Profile, weights Weights, now time.Time, k int) []Prediction {
	if k <= 0 {
		return nil
	}
	preds := make([]Prediction, 0, len(files))

	for _, f := range files {
		sRec, sFreq, sCos, sTime := CalculateFeatures(f, prof, now)

		score := weights.Cos*sCos + weights.Rec*sRec + weights.Freq*sFreq + weights.Time*sTime
		preds = append(preds, Prediction{
			Path:   f.Path,
			Score:  score,
			Reason: reasonFor(weights.Rec*sRec, weights.Freq*sFreq, weights.Cos*sCos, weights.Time*sTime),
		})
	}

	sort.SliceStable(preds, func(i, j int) bool { return preds[i].Score > preds[j].Score })
	if len(preds) > k {
		preds = preds[:k]
	}
	return preds
}

// CalculateFeatures tính toán 4 yếu tố feature gốc cho 1 file.
func CalculateFeatures(f FileCandidate, prof Profile, now time.Time) (rec, freq, cos, tMatch float64) {
	lambda := math.Ln2 / recHalfLifeHours

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
	rec = math.Exp(-lambda * ageH)

	// frequency: BM25 saturation count/(count+k)
	if st := prof.FileStats[f.Path]; st != nil {
		c := float64(st.OpenCount)
		freq = c / (c + freqK)
	}

	// cosine: dot product (vector đã ~chuẩn hoá); max(0,·)
	if len(prof.InterestVector) > 0 {
		var dot float64
		for i := range f.Vector {
			if i < len(prof.InterestVector) {
				dot += float64(f.Vector[i]) * float64(prof.InterestVector[i])
			}
		}
		if dot > 0 {
			cos = dot
		}
	}

	// time: time profile match
	if prof.TimeProfile != nil && prof.TimeProfile[f.Path] != nil {
		// Find peak hour
		maxCount := 0
		peakHour := -1
		for h, c := range prof.TimeProfile[f.Path] {
			if c > maxCount {
				maxCount = c
				peakHour = h
			}
		}
		if peakHour >= 0 {
			hNow := now.Hour()
			diff1 := math.Abs(float64(hNow - peakHour))
			diff2 := 24.0 - diff1
			delta := diff1
			if diff2 < diff1 {
				delta = diff2
			}
			tMatch = math.Exp(-(delta * delta) / (2 * timeSigma * timeSigma))
		}
	}

	return
}

// reasonFor trả lý do ngắn dựa yếu tố trội nhất.
func reasonFor(rec, freq, cos, tMatch float64) string {
	if tMatch > rec && tMatch > freq && tMatch > cos {
		return "đúng nhịp làm việc giờ này"
	}
	if rec >= freq && rec >= cos {
		return "vừa truy cập gần đây"
	}
	if freq >= cos {
		return "bạn hay mở file này"
	}
	return "liên quan việc bạn đang làm"
}

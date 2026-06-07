package intent

import (
	"math"
	"time"
)

// FileStat là thống kê per-file (cho frequency/recency).
type FileStat struct {
	OpenCount  int
	LastOpened time.Time
}

// Profile là hồ sơ ngữ nghĩa người dùng tại 1 thời điểm.
type Profile struct {
	InterestVector []float32            // tâm điểm quan tâm (trung bình có trọng số, decay)
	FileStats      map[string]*FileStat // path -> thống kê
}

// interestHalfLife = 2h: hành động cũ hơn nhanh chóng mất giá trị ("session mềm").
const interestHalfLifeHours = 2.0

// BuildProfile xây profile từ events. KHÔNG có interest vector (cần embed) — chỉ
// file stats. Dùng khi không cần ngữ nghĩa (vd test, hoặc predictor recency-only).
func BuildProfile(events []Event, now time.Time) Profile {
	return BuildProfileWithEmbed(events, now, nil)
}

// BuildProfileWithEmbed xây profile đầy đủ. embed(query) trả vector cho 1 search
// query (nil = bỏ qua interest vector). Trọng số mỗi event = exp(-ln2 * age/2h).
func BuildProfileWithEmbed(events []Event, now time.Time, embed func(string) []float32) Profile {
	p := Profile{FileStats: make(map[string]*FileStat)}
	lambda := math.Ln2 / interestHalfLifeHours

	var sum []float32
	for _, e := range events {
		switch e.Type {
		case EventOpen, EventSuggestionClick:
			if e.Path != "" {
				st := p.FileStats[e.Path]
				if st == nil {
					st = &FileStat{}
					p.FileStats[e.Path] = st
				}
				st.OpenCount++
				if e.Time.After(st.LastOpened) {
					st.LastOpened = e.Time
				}
			}
		case EventSearch:
			if embed != nil && e.Query != "" {
				v := embed(e.Query)
				ageH := now.Sub(e.Time).Hours()
				if ageH < 0 {
					ageH = 0
				}
				w := float32(math.Exp(-lambda * ageH))
				if sum == nil {
					sum = make([]float32, len(v))
				}
				for i := range v {
					if i < len(sum) {
						sum[i] += w * v[i]
					}
				}
			}
		}
	}
	p.InterestVector = l2normalize(sum)
	return p
}

func l2normalize(v []float32) []float32 {
	if len(v) == 0 {
		return v
	}
	var n float64
	for _, x := range v {
		n += float64(x) * float64(x)
	}
	n = math.Sqrt(n)
	if n == 0 {
		return v
	}
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) / n)
	}
	return out
}

package intent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Weights định nghĩa trọng số cho các yếu tố dự đoán
type Weights struct {
	Cos  float64 `json:"cos"`
	Rec  float64 `json:"rec"`
	Freq float64 `json:"freq"`
	Time float64 `json:"time"`
}

// DefaultWeights trả về trọng số mặc định (tổng = 1.0)
func DefaultWeights() Weights {
	return Weights{
		Cos:  0.20,
		Rec:  0.60,
		Freq: 0.10,
		Time: 0.10,
	}
}

var weightsMu sync.Mutex

// SaveWeights lưu trọng số vào file
func SaveWeights(dir string, w Weights) error {
	weightsMu.Lock()
	defer weightsMu.Unlock()
	
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	p := filepath.Join(dir, "weights.json")
	data, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0644)
}

// LoadWeights đọc trọng số từ file, nếu lỗi trả về DefaultWeights
func LoadWeights(dir string) Weights {
	weightsMu.Lock()
	defer weightsMu.Unlock()

	p := filepath.Join(dir, "weights.json")
	data, err := os.ReadFile(p)
	if err != nil {
		return DefaultWeights()
	}
	var w Weights
	if err := json.Unmarshal(data, &w); err != nil {
		return DefaultWeights()
	}
	return w
}

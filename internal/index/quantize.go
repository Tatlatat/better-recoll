package index

import (
	"math"
	"sort"
)

// QuantizeInt8 performs symmetric quantization:
// scale = max(abs(vec))/127, q[i] = round(vec[i]/scale).
// Returns the int8 slice and the scale.
func QuantizeInt8(vec []float32) ([]int8, float32) {
	if len(vec) == 0 {
		return nil, 0
	}

	var maxAbs float32
	for _, x := range vec {
		abs := x
		if abs < 0 {
			abs = -abs
		}
		if abs > maxAbs {
			maxAbs = abs
		}
	}

	if maxAbs == 0 {
		return make([]int8, len(vec)), 0
	}

	scale := maxAbs / 127.0
	q := make([]int8, len(vec))
	for i, x := range vec {
		val := math.Round(float64(x / scale))
		if val > 127 {
			val = 127
		} else if val < -127 {
			val = -127
		}
		q[i] = int8(val)
	}

	return q, scale
}

// DequantizeInt8 performs the inverse operation of QuantizeInt8.
func DequantizeInt8(q []int8, scale float32) []float32 {
	if q == nil {
		return nil
	}

	vec := make([]float32, len(q))
	for i, x := range q {
		vec[i] = float32(x) * scale
	}
	return vec
}

// Int8VectorIndex stores quantized int8 vectors with their scales to save memory.
type Int8VectorIndex struct {
	dim     int
	ids     []int64
	vectors [][]int8
	scales  []float32
}

// NewInt8VectorIndex creates and initializes a new Int8VectorIndex with the specified vector dimension.
func NewInt8VectorIndex(dim int) *Int8VectorIndex {
	return &Int8VectorIndex{
		dim:     dim,
		ids:     make([]int64, 0),
		vectors: make([][]int8, 0),
		scales:  make([]float32, 0),
	}
}

// Add quantizes and stores the vector as int8 + scale.
func (v *Int8VectorIndex) Add(id int64, vec []float32) {
	q, scale := QuantizeInt8(vec)
	v.ids = append(v.ids, id)
	v.vectors = append(v.vectors, q)
	v.scales = append(v.scales, scale)
}

// Search searches the vector index for the nearest vectors to the query vector, returning the top-k results.
// It scores by int8 dot-product times scales.
func (v *Int8VectorIndex) Search(query []float32, k int) []Result {
	if len(v.ids) == 0 || k <= 0 {
		return nil
	}

	qQuery, scaleQuery := QuantizeInt8(query)
	results := make([]Result, len(v.ids))

	for i, id := range v.ids {
		qStored := v.vectors[i]
		scaleStored := v.scales[i]

		var dot int32
		limit := len(qQuery)
		if len(qStored) < limit {
			limit = len(qStored)
		}
		for j := 0; j < limit; j++ {
			dot += int32(qQuery[j]) * int32(qStored[j])
		}

		score := float32(dot) * scaleQuery * scaleStored
		results[i] = Result{
			ID:    id,
			Score: score,
		}
	}

	// Sort desc by Score. If scores are equal, sort by ID to make it deterministic.
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].ID < results[j].ID
		}
		return results[i].Score > results[j].Score
	})

	if len(results) > k {
		results = results[:k]
	}
	return results
}

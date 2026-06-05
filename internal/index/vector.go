package index

import (
	"sort"
)

// VectorIndex implements a flat-scan vector index using cosine similarity (via dot product of pre-normalized vectors).
type VectorIndex struct {
	dim     int
	ids     []int64
	vectors [][]float32
}

// NewVectorIndex creates and initializes a new VectorIndex with the specified vector dimension.
func NewVectorIndex(dim int) *VectorIndex {
	return &VectorIndex{
		dim:     dim,
		ids:     make([]int64, 0),
		vectors: make([][]float32, 0),
	}
}

// Add adds a vector to the index.
func (v *VectorIndex) Add(id int64, vec []float32) {
	// Copy the vector to prevent external modification issues.
	vecCopy := make([]float32, len(vec))
	copy(vecCopy, vec)
	v.ids = append(v.ids, id)
	v.vectors = append(v.vectors, vecCopy)
}

// Search searches the vector index for the nearest vectors to the query vector, returning the top-k results.
// The query vector and stored vectors are assumed to be pre-normalized, so similarity is computed via dot-product.
func (v *VectorIndex) Search(query []float32, k int) []Result {
	if len(v.ids) == 0 || k <= 0 {
		return nil
	}

	results := make([]Result, len(v.ids))
	for i, id := range v.ids {
		storedVec := v.vectors[i]
		var score float32

		// Compute dot product
		limit := len(query)
		if len(storedVec) < limit {
			limit = len(storedVec)
		}
		for j := 0; j < limit; j++ {
			score += query[j] * storedVec[j]
		}

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

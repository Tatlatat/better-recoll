package index

import (
	"math"
	"math/rand"
	"testing"
)

// normalizeHelper computes the L2 normalized version of a slice.
func normalizeHelper(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x * x)
	}
	if sum == 0 {
		return v
	}
	n := float32(math.Sqrt(sum))
	out := make([]float32, len(v))
	for i := range v {
		out[i] = v[i] / n
	}
	return out
}

func TestInt8QuantizeRoundtrip(t *testing.T) {
	rng := rand.New(rand.NewSource(42))

	// Test multiple dimensions and vectors
	dims := []int{3, 4, 64, 128, 256}
	for _, dim := range dims {
		for i := 0; i < 10; i++ {
			vec := make([]float32, dim)
			for j := 0; j < dim; j++ {
				vec[j] = rng.Float32()*2.0 - 1.0
			}
			vec = normalizeHelper(vec)

			q, scale := QuantizeInt8(vec)
			deq := DequantizeInt8(q, scale)

			// Calculate L2 error
			var diffSum float64
			for j := 0; j < dim; j++ {
				diff := float64(vec[j] - deq[j])
				diffSum += diff * diff
			}
			l2Err := math.Sqrt(diffSum)

			if l2Err >= 0.01 {
				t.Errorf("Dim %d: L2 error of roundtrip = %f, want < 0.01 (1%%)", dim, l2Err)
			}
		}
	}
}

func TestInt8VectorIndexSearch(t *testing.T) {
	rng := rand.New(rand.NewSource(100))
	dim := 128
	numVecs := 50

	vIdx := NewVectorIndex(dim)
	i8Idx := NewInt8VectorIndex(dim)

	// Populate both indexes with the same vectors
	for i := 0; i < numVecs; i++ {
		vec := make([]float32, dim)
		for j := 0; j < dim; j++ {
			vec[j] = rng.Float32()*2.0 - 1.0
		}
		vec = normalizeHelper(vec)
		id := int64(i + 1)
		vIdx.Add(id, vec)
		i8Idx.Add(id, vec)
	}

	// Run multiple queries and compare the top-1 search result
	for q := 0; q < 20; q++ {
		queryVec := make([]float32, dim)
		for j := 0; j < dim; j++ {
			queryVec[j] = rng.Float32()*2.0 - 1.0
		}
		queryVec = normalizeHelper(queryVec)

		vResults := vIdx.Search(queryVec, 5)
		i8Results := i8Idx.Search(queryVec, 5)

		if len(vResults) == 0 || len(i8Results) == 0 {
			t.Fatalf("Search returned 0 results: len(vResults)=%d, len(i8Results)=%d", len(vResults), len(i8Results))
		}

		// Top-1 must match on ID
		if vResults[0].ID != i8Results[0].ID {
			t.Errorf("Query %d: top-1 nearest neighbor ID mismatch. Plain VectorIndex: %d (score %f), Int8VectorIndex: %d (score %f)",
				q, vResults[0].ID, vResults[0].Score, i8Results[0].ID, i8Results[0].Score)
		}
	}
}

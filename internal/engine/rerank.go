package engine

import (
	"fmt"
	"math"
	"sort"

	"sfs/internal/normalize"
)

// RankedResults holds search results partitioned into Exact (prob >= 0.5)
// and Suggest (prob < 0.5) buckets after cross-encoder reranking.
type RankedResults struct {
	Exact   []Result
	Suggest []Result
}

// sigmoid applies sigmoid(logit) to get a 0..1 probability.
func sigmoid(x float32) float32 {
	return float32(1.0 / (1.0 + math.Exp(float64(-x))))
}

// SearchRanked retrieves top-K results using the vector and BM25 index,
// reranks candidates using the cross-encoder model, and buckets the results.
func (e *Engine) SearchRanked(query string, k int) (RankedResults, error) {
	if k <= 0 {
		return RankedResults{}, nil
	}

	// Embed query
	queryEmbs, err := e.embedder.Embed([]string{query})
	if err != nil {
		return RankedResults{}, fmt.Errorf("failed to embed query: %w", err)
	}
	queryVector := queryEmbs[0]

	e.mu.Lock()
	defer e.mu.Unlock()

	candidateK := e.rerankK
	if candidateK <= 0 {
		candidateK = 20
	}

	// Get candidate results from vector and BM25 search
	vectorResults := e.vindex.Search(queryVector, candidateK)
	bm25Results := e.bm25.Search(normalize.Normalize(query), candidateK)

	// Merge candidate chunk IDs and dedupe
	candidateIDs := make(map[int64]bool)
	for _, r := range bm25Results {
		candidateIDs[r.ID] = true
	}
	for _, r := range vectorResults {
		candidateIDs[r.ID] = true
	}

	if len(candidateIDs) == 0 {
		return RankedResults{}, nil
	}

	// Load candidates
	var candidates []Result
	for id := range candidateIDs {
		ch, err := e.store.GetChunk(id)
		if err != nil {
			return RankedResults{}, fmt.Errorf("failed to retrieve chunk %d: %w", id, err)
		}
		candidates = append(candidates, Result{
			FilePath: ch.FilePath,
			Text:     ch.Text,
			ChunkID:  id,
		})
	}

	// Score candidates using reranker
	texts := make([]string, len(candidates))
	for i, c := range candidates {
		texts[i] = c.Text
	}

	scores, err := e.reranker.Score(query, texts)
	if err != nil {
		return RankedResults{}, fmt.Errorf("failed to score candidates with reranker: %w", err)
	}

	for i := range candidates {
		candidates[i].Score = scores[i]
	}

	// Sort candidates by reranker score desc, tie-breaker by ChunkID
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].ChunkID < candidates[j].ChunkID
		}
		return candidates[i].Score > candidates[j].Score
	})

	// Take top k candidates
	if len(candidates) > k {
		candidates = candidates[:k]
	}

	// Bucket candidates based on sigmoid(logit) threshold >= 0.5
	var exact []Result
	var suggest []Result
	for _, c := range candidates {
		prob := sigmoid(c.Score)
		c.Score = prob
		if prob >= 0.5 {
			exact = append(exact, c)
		} else {
			suggest = append(suggest, c)
		}
	}

	return RankedResults{
		Exact:   exact,
		Suggest: suggest,
	}, nil
}

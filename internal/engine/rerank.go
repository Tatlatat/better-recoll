package engine

import (
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"sfs/internal/normalize"
)

// ExactThreshold: ngưỡng sigmoid(logit reranker) để vào ô "Chính xác".
// Calibrate bằng số đo thật trên tài liệu xây dựng VN: query ĐÚNG cho score
// 0.14–0.94 (tùy độ khớp), query KHÔNG liên quan cho ~0.00002 (cross-encoder
// loại tuyệt đối). Khoảng cách 3+ bậc → đặt 0.05: bắt cả kết quả đúng yếu, mà
// rác (~0.00002) vẫn rớt rõ xuống Gợi ý.
const ExactThreshold = 0.05

// SuggestThreshold: ngưỡng tối thiểu để đưa kết quả vào ô "Gợi ý".
// Kết quả dưới ngưỡng này sẽ bị loại bỏ hoàn toàn (dropped) để tránh hiển thị kết quả rác.
const SuggestThreshold = 0.01

// RankedResults holds search results partitioned into Exact (prob >= ExactThreshold)
// and Suggest (prob < ExactThreshold) buckets after cross-encoder reranking.
type RankedResults struct {
	Exact   []Result
	Suggest []Result
}

// sigmoid applies sigmoid(logit) to get a 0..1 probability.
func sigmoid(x float32) float32 {
	return float32(1.0 / (1.0 + math.Exp(float64(-x))))
}

// SearchFast is the STAGE-1 search: embed + vector/BM25 retrieve + dedup by
// file, WITHOUT the cross-encoder rerank. It returns in ~40ms so the UI can
// show approximate results instantly while the user is still typing. The
// reranked (precise) results follow from SearchRanked. Results are ordered by
// cosine similarity and bucketed coarsely (all into Exact since we have no
// calibrated reranker probability yet).
func (e *Engine) SearchFast(query string, k int) (RankedResults, error) {
	if k <= 0 {
		return RankedResults{}, nil
	}

	queryEmbs, err := e.embedder.Embed([]string{query})
	if err != nil {
		return RankedResults{}, fmt.Errorf("failed to embed query: %w", err)
	}
	queryVector := queryEmbs[0]

	e.mu.Lock()
	defer e.mu.Unlock()

	fetchK := k * 8
	if fetchK < 40 {
		fetchK = 40
	}
	vectorResults := e.vindex.Search(queryVector, fetchK)
	bm25Results := e.bm25.Search(normalize.Normalize(query), fetchK)
	if len(vectorResults) == 0 && len(bm25Results) == 0 {
		return RankedResults{}, nil
	}

	seenFile := make(map[string]bool)
	var out []Result
	add := func(id int64, score float32) {
		if len(out) >= k {
			return
		}
		ch, err := e.store.GetChunk(id)
		if err != nil || seenFile[ch.FilePath] {
			return
		}
		seenFile[ch.FilePath] = true
		out = append(out, Result{FilePath: ch.FilePath, Text: ch.Text, ChunkID: id, Score: score})
	}
	maxLen := len(vectorResults)
	if len(bm25Results) > maxLen {
		maxLen = len(bm25Results)
	}
	for i := 0; i < maxLen && len(out) < k; i++ {
		if i < len(vectorResults) {
			add(vectorResults[i].ID, vectorResults[i].Score)
		}
		if i < len(bm25Results) {
			add(bm25Results[i].ID, 0)
		}
	}

	// Stage-1 has no calibrated probability; surface all as approximate Exact.
	return RankedResults{Exact: out}, nil
}

// SearchRanked retrieves top-K results using the vector and BM25 index,
// reranks candidates using the cross-encoder model, and buckets the results.
func (e *Engine) SearchRanked(query string, k int) (RankedResults, error) {
	if k <= 0 {
		return RankedResults{}, nil
	}

	t0 := time.Now()

	// Embed query
	queryEmbs, err := e.embedder.Embed([]string{query})
	if err != nil {
		return RankedResults{}, fmt.Errorf("failed to embed query: %w", err)
	}
	queryVector := queryEmbs[0]
	tEmbed := time.Since(t0)

	e.mu.Lock()
	defer e.mu.Unlock()
	tLock := time.Since(t0) - tEmbed

	candidateK := e.rerankK
	if candidateK <= 0 {
		candidateK = 20
	}

	// Over-fetch from each retriever: a dirty/duplicate-heavy index can return
	// many copies of the same chunk, so ask for far more than candidateK to be
	// sure we surface candidateK DISTINCT files after dedup. fetchK is generous
	// because retrieval (~40ms) is cheap; the reranker (the expensive stage)
	// still only sees candidateK distinct-file candidates.
	fetchK := candidateK * 8
	if fetchK < 40 {
		fetchK = 40
	}

	tRetrieve0 := time.Now()
	vectorResults := e.vindex.Search(queryVector, fetchK)
	bm25Results := e.bm25.Search(normalize.Normalize(query), fetchK)
	tRetrieve := time.Since(tRetrieve0)

	if len(vectorResults) == 0 && len(bm25Results) == 0 {
		return RankedResults{}, nil
	}

	// Build candidate pool deduped BY FILE PATH, interleaving the two retrievers
	// so both vector (semantic) and BM25 (keyword) get representation. Keep the
	// FIRST (highest-ranked) chunk seen per file. Stop once we have candidateK
	// distinct files — this is the fix that stops duplicate chunks from
	// crowding the real answer out of the pool.
	seenFile := make(map[string]bool)
	var candidates []Result
	addCandidate := func(id int64) bool {
		if len(candidates) >= candidateK {
			return false
		}
		ch, err := e.store.GetChunk(id)
		if err != nil {
			return true // skip this id, keep going
		}
		if seenFile[ch.FilePath] {
			return true
		}
		seenFile[ch.FilePath] = true
		candidates = append(candidates, Result{
			FilePath: ch.FilePath,
			Text:     ch.Text,
			ChunkID:  id,
		})
		return true
	}

	maxLen := len(vectorResults)
	if len(bm25Results) > maxLen {
		maxLen = len(bm25Results)
	}
	for i := 0; i < maxLen && len(candidates) < candidateK; i++ {
		if i < len(vectorResults) {
			if !addCandidate(vectorResults[i].ID) {
				break
			}
		}
		if i < len(bm25Results) {
			if !addCandidate(bm25Results[i].ID) {
				break
			}
		}
	}

	if len(candidates) == 0 {
		return RankedResults{}, nil
	}

	if os.Getenv("SFS_PROFILE") != "" {
		log.Printf("[STAGE1] q=%q distinct-file pool (%d): fetched v=%d bm25=%d", query, len(candidates), len(vectorResults), len(bm25Results))
		for i, c := range candidates {
			if i >= 5 {
				break
			}
			log.Printf("  s1#%d file=%s", i, filepath.Base(c.FilePath))
		}
	}

	// Score candidates using reranker
	texts := make([]string, len(candidates))
	for i, c := range candidates {
		texts[i] = c.Text
	}

	tRerank0 := time.Now()
	scores, err := e.reranker.Score(query, texts)
	if err != nil {
		return RankedResults{}, fmt.Errorf("failed to score candidates with reranker: %w", err)
	}
	tRerank := time.Since(tRerank0)

	if os.Getenv("SFS_PROFILE") != "" {
		log.Printf("[PROFILE] q=%q cands=%d | embed=%v lock-wait=%v retrieve=%v rerank=%v total=%v",
			query, len(candidates), tEmbed.Round(time.Millisecond), tLock.Round(time.Millisecond),
			tRetrieve.Round(time.Millisecond), tRerank.Round(time.Millisecond), time.Since(t0).Round(time.Millisecond))
	}

	for i := range candidates {
		candidates[i].Score = scores[i]
	}

	// Apply router boosts if applicable
	if e.Router != nil {
		boosts := e.Router.Boost(query)
		for i := range candidates {
			for label, boost := range boosts {
				if strings.Contains(strings.ToLower(candidates[i].FilePath), strings.ToLower(label)) {
					candidates[i].Score += boost
				}
			}
		}
	}

	// Sort candidates by reranker score desc, tie-breaker by ChunkID
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].ChunkID < candidates[j].ChunkID
		}
		return candidates[i].Score > candidates[j].Score
	})

	// Deduplicate candidates by FilePath, keeping only the highest-scoring result per file path.
	// Since candidates is already sorted descending, the first occurrence of each FilePath is the highest scoring one.
	seen := make(map[string]bool)
	var deduped []Result
	for _, c := range candidates {
		if !seen[c.FilePath] {
			seen[c.FilePath] = true
			deduped = append(deduped, c)
		}
	}
	candidates = deduped

	// Take top k candidates
	if len(candidates) > k {
		candidates = candidates[:k]
	}

	if os.Getenv("SFS_PROFILE") != "" {
		log.Printf("[CANDIDATES] q=%q ranked:", query)
		for i, c := range candidates {
			log.Printf("  #%d prob=%.4f logit=%.3f file=%s", i, sigmoid(c.Score), c.Score, filepath.Base(c.FilePath))
		}
	}

	// Bucket candidates based on sigmoid(logit) vs ExactThreshold and SuggestThreshold
	var exact []Result
	var suggest []Result
	for _, c := range candidates {
		prob := sigmoid(c.Score)
		c.Score = prob
		if prob >= ExactThreshold {
			exact = append(exact, c)
		} else if prob >= SuggestThreshold {
			suggest = append(suggest, c)
		}
	}

	return RankedResults{
		Exact:   exact,
		Suggest: suggest,
	}, nil
}

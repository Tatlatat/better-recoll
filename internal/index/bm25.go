package index

import (
	"math"
	"sort"
	"strings"
)

// Result represents a search result with a document ID and its score.
type Result struct {
	ID    int64
	Score float32
}

// BM25 implements the Okapi BM25 information retrieval algorithm.
type BM25 struct {
	docs     map[int64]map[string]int // docId -> term -> term frequency in doc
	docLens  map[int64]int            // docId -> document length (total words)
	df       map[string]int           // term -> document frequency (number of docs containing term)
	idf      map[string]float64       // term -> inverse document frequency
	avgdl    float64                  // average document length
	docCount int                      // total number of documents
}

// NewBM25 creates and initializes a new BM25 index.
func NewBM25() *BM25 {
	return &BM25{
		docs:    make(map[int64]map[string]int),
		docLens: make(map[int64]int),
		df:      make(map[string]int),
		idf:     make(map[string]float64),
	}
}

// tokenize splits a string by whitespace and converts tokens to lowercase.
func tokenize(text string) []string {
	return strings.Fields(strings.ToLower(text))
}

// Add adds a document to the BM25 index.
func (b *BM25) Add(id int64, text string) {
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return
	}

	b.docCount++
	b.docLens[id] = len(tokens)

	wordFreqs := make(map[string]int)
	for _, t := range tokens {
		wordFreqs[t]++
	}

	b.docs[id] = wordFreqs

	for word := range wordFreqs {
		b.df[word]++
	}
}

// Build finalizes idf and avgdl parameters after all documents have been added.
func (b *BM25) Build() {
	if b.docCount == 0 {
		b.avgdl = 0
		return
	}

	totalLen := 0
	for _, l := range b.docLens {
		totalLen += l
	}
	b.avgdl = float64(totalLen) / float64(b.docCount)

	// Calculate IDF for each term using standard Lucene BM25 IDF formula:
	// idf = ln(1 + (N - df + 0.5) / (df + 0.5))
	// This ensures IDF is always positive.
	b.idf = make(map[string]float64)
	N := float64(b.docCount)
	for word, dfVal := range b.df {
		df := float64(dfVal)
		idfVal := math.Log(1.0 + (N-df+0.5)/(df+0.5))
		if idfVal < 0 {
			idfVal = 0
		}
		b.idf[word] = idfVal
	}
}

// Search searches the BM25 index for the query string and returns the top-k results.
// Score parameters: k1 = 1.5, b = 0.75.
func (b *BM25) Search(query string, k int) []Result {
	tokens := tokenize(query)
	if len(tokens) == 0 || b.docCount == 0 || k <= 0 {
		return nil
	}

	scores := make(map[int64]float32)
	k1 := 1.5
	bParam := 0.75

	for _, token := range tokens {
		idfVal, ok := b.idf[token]
		if !ok {
			continue
		}

		for docID, wordFreqs := range b.docs {
			tf := float64(wordFreqs[token])
			if tf == 0 {
				continue
			}

			docLen := float64(b.docLens[docID])
			denom := tf + k1*(1.0-bParam+bParam*(docLen/b.avgdl))
			termScore := idfVal * (tf * (k1 + 1.0)) / denom
			scores[docID] += float32(termScore)
		}
	}

	results := make([]Result, 0, len(scores))
	for id, score := range scores {
		if score > 0 {
			results = append(results, Result{ID: id, Score: score})
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

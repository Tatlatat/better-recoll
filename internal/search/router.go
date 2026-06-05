package search

import (
	"strings"
	"sync"

	"sfs/internal/normalize"
)

// Router maps a template/document-type label to a list of keywords.
type Router struct {
	mu       sync.RWMutex
	registry map[string][]string
}

// NewRouter creates a new Router instance.
func NewRouter() *Router {
	return &Router{
		registry: make(map[string][]string),
	}
}

// Register registers keywords for a template/document-type label.
func (r *Router) Register(label string, keywords []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Store pre-normalized keywords for efficient matching.
	normKeywords := make([]string, 0, len(keywords))
	for _, kw := range keywords {
		normKw := normalize.Normalize(kw)
		if normKw != "" {
			normKeywords = append(normKeywords, normKw)
		}
	}
	r.registry[label] = normKeywords
}

// Boost returns a SOFT score bonus per label when the normalized query contains
// the label keywords. This is a SOFT signal (additive bonus), it must NEVER exclude
// anything, just return bonus weights (0.1 per matched keyword).
func (r *Router) Boost(query string) map[string]float32 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	normQuery := normalize.Normalize(query)
	boosts := make(map[string]float32)

	for label, keywords := range r.registry {
		var score float32
		for _, kw := range keywords {
			if strings.Contains(normQuery, kw) {
				score += 0.1
			}
		}
		if score > 0 {
			boosts[label] = score
		}
	}

	return boosts
}

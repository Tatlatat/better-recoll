package dedupe

import (
	"hash/fnv"
	"strings"
)

// DedupeFinder finds boilerplate segments (text that repeats across many files).
type DedupeFinder struct {
	minDocs int
	counts  map[uint64]int
}

// New creates a new DedupeFinder with the specified minimum document occurrence count threshold.
func New(minDocs int) *DedupeFinder {
	return &DedupeFinder{
		minDocs: minDocs,
		counts:  make(map[uint64]int),
	}
}

// fingerprint computes a normalized hash of the segment:
// lowercase, collapse whitespace, then FNV-1a hash.
func (d *DedupeFinder) fingerprint(seg string) uint64 {
	// Lowercase and collapse whitespace by splitting into fields and joining with a single space.
	normalized := strings.Join(strings.Fields(strings.ToLower(seg)), " ")
	h := fnv.New64a()
	_, _ = h.Write([]byte(normalized))
	return h.Sum64()
}

// Add records one occurrence of a segment.
func (d *DedupeFinder) Add(seg string) {
	fp := d.fingerprint(seg)
	d.counts[fp]++
}

// Build finalizes the deduplication structure.
func (d *DedupeFinder) Build() {
	// Currently a no-op as counts are maintained dynamically,
	// but satisfies the API requirements.
}

// IsBoilerplate returns true if that segment fingerprint occurred >= minDocs times.
func (d *DedupeFinder) IsBoilerplate(seg string) bool {
	fp := d.fingerprint(seg)
	return d.counts[fp] >= d.minDocs
}

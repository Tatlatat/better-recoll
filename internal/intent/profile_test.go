package intent

import (
	"testing"
	"time"
)

func TestFileStatsCountsOpens(t *testing.T) {
	now := time.Now()
	events := []Event{
		{Time: now, Type: EventOpen, Path: "/a.txt"},
		{Time: now, Type: EventOpen, Path: "/a.txt"},
		{Time: now, Type: EventOpen, Path: "/b.txt"},
	}
	p := BuildProfile(events, now)
	if p.FileStats["/a.txt"].OpenCount != 2 {
		t.Fatalf("a phải mở 2 lần, được %d", p.FileStats["/a.txt"].OpenCount)
	}
	if p.FileStats["/b.txt"].OpenCount != 1 {
		t.Fatalf("b phải mở 1 lần, được %d", p.FileStats["/b.txt"].OpenCount)
	}
}

func TestInterestVectorWeightsRecent(t *testing.T) {
	now := time.Now()
	// 2 search: 1 mới (vector A), 1 cũ 10h (vector B). Interest phải nghiêng về A.
	events := []Event{
		{Time: now.Add(-10 * time.Hour), Type: EventSearch, Query: "cũ"},
		{Time: now, Type: EventSearch, Query: "mới"},
	}
	// embedFn giả: "mới"→[1,0], "cũ"→[0,1]
	embed := func(q string) []float32 {
		if q == "mới" {
			return []float32{1, 0}
		}
		return []float32{0, 1}
	}
	p := BuildProfileWithEmbed(events, now, embed)
	// interest[0] (thành phần "mới") phải > interest[1] (thành phần "cũ")
	if p.InterestVector[0] <= p.InterestVector[1] {
		t.Fatalf("interest phải nghiêng về 'mới': %v", p.InterestVector)
	}
}

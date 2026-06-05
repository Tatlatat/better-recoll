package search

import (
	"testing"
)

func TestRouterBoost(t *testing.T) {
	r := NewRouter()
	r.Register("bienban", []string{"bien ban", "nghiem thu"})

	// Query: "biên bản nghiệm thu" should match both "bien ban" and "nghiem thu".
	// Expected boost for "bienban" should be 0.2 (0.1 for "bien ban" + 0.1 for "nghiem thu").
	boosts := r.Boost("biên bản nghiệm thu")

	val, exists := boosts["bienban"]
	if !exists {
		t.Fatalf("expected boost for 'bienban', but none found")
	}

	// We expect positive boost, around 0.2
	expected := float32(0.2)
	if val < 0.19 || val > 0.21 {
		t.Errorf("expected boost for 'bienban' to be around %f, got %f", expected, val)
	}

	// Additional test: query with partially matching keywords or no match
	boosts2 := r.Boost("biên bản")
	val2, exists2 := boosts2["bienban"]
	if !exists2 {
		t.Fatalf("expected boost for 'bienban' with partial match, but none found")
	}
	if val2 < 0.09 || val2 > 0.11 {
		t.Errorf("expected boost for 'bienban' to be around 0.1, got %f", val2)
	}

	boosts3 := r.Boost("hợp đồng lao động")
	if len(boosts3) > 0 {
		t.Errorf("expected no boosts for unmatched query, got %v", boosts3)
	}
}

package normalize

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Normalize removes Vietnamese diacritics and lowercases the input string.
// It uses golang.org/x/text/unicode/norm (NFD decomposition) then drops
// combining marks (unicode.Mn range) using golang.org/x/text/runes and transform,
// and finally lowercases using strings.ToLower.
// It explicitly handles Đ/đ -> d as NFD does not decompose đ.
func Normalize(s string) string {
	// Explicitly map Đ and đ to d.
	s = strings.ReplaceAll(s, "Đ", "d")
	s = strings.ReplaceAll(s, "đ", "d")

	// Apply NFD decomposition, drop unicode.Mn combining marks, and reconstruct.
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	res, _, _ := transform.String(t, s)

	return strings.ToLower(res)
}

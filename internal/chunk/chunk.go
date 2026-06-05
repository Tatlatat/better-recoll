package chunk

import (
	"unicode"

	"sfs/internal/chunk/types"
)

// Chunk splits text into pieces of about size runes (not bytes), preferring to
// break at sentence boundaries (. ! ? newline) near the size limit, never splitting mid-word.
func Chunk(text string, size int) []types.Chunk {
	if size <= 0 {
		size = 1
	}

	runes := []rune(text)
	var chunks []types.Chunk
	startIdx := 0

	for startIdx < len(runes) {
		// If the remaining text is under or equal to size, take all of it.
		if len(runes)-startIdx <= size {
			chunkText, left, right := trimRunes(runes, startIdx, len(runes))
			if left < right {
				chunks = append(chunks, types.Chunk{
					Text:   chunkText,
					Offset: left,
				})
			}
			break
		}

		// Otherwise, find a split point in the range (startIdx, startIdx+size]
		limit := startIdx + size
		splitIdx := -1

		// 1. Search for a sentence boundary in [startIdx + size/2, limit] searching backwards
		lowerBound := startIdx + size/2
		if lowerBound <= startIdx {
			lowerBound = startIdx + 1
		}
		for i := limit; i >= lowerBound; i-- {
			if isSentenceBoundary(runes, i) && isWordBoundary(runes, i) {
				splitIdx = i
				break
			}
		}

		// 2. If no sentence boundary in that range, search for any word boundary in [startIdx + 1, limit] searching backwards
		if splitIdx == -1 {
			for i := limit; i >= startIdx+1; i-- {
				if isWordBoundary(runes, i) {
					splitIdx = i
					break
				}
			}
		}

		// 3. If no word boundary in [startIdx + 1, limit], we search forwards from limit to find the first word boundary
		if splitIdx == -1 {
			for i := limit; i <= len(runes); i++ {
				if isWordBoundary(runes, i) {
					splitIdx = i
					break
				}
			}
		}

		// Extract chunk
		chunkText, left, right := trimRunes(runes, startIdx, splitIdx)
		if left < right {
			chunks = append(chunks, types.Chunk{
				Text:   chunkText,
				Offset: left,
			})
		}

		// Move startIdx to splitIdx
		startIdx = splitIdx
	}

	return chunks
}

func isLetterOrDigit(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func isWordBoundary(runes []rune, i int) bool {
	if i <= 0 || i >= len(runes) {
		return true
	}
	return !isLetterOrDigit(runes[i-1]) || !isLetterOrDigit(runes[i])
}

func isSentenceBoundary(runes []rune, i int) bool {
	if i <= 0 || i > len(runes) {
		return false
	}
	r := runes[i-1]
	if r == '\n' || r == '\r' {
		return true
	}
	if r == '.' || r == '!' || r == '?' {
		if i == len(runes) {
			return true
		}
		next := runes[i]
		return unicode.IsSpace(next) || unicode.IsPunct(next) || !isLetterOrDigit(next)
	}
	return false
}

// trimRunes returns the trimmed string and the indices of the first and last non-space runes.
func trimRunes(runes []rune, start, end int) (string, int, int) {
	left := start
	for left < end && unicode.IsSpace(runes[left]) {
		left++
	}
	right := end
	for right > left && unicode.IsSpace(runes[right-1]) {
		right--
	}
	if left >= right {
		return "", left, right
	}
	return string(runes[left:right]), left, right
}

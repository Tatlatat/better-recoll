package intent

// RunReplay replays events and returns HitRate@5 and MRR@5.
func RunReplay(events []Event, files []FileCandidate) (hitRate float64, mrr float64) {
	var hits int
	var sumRR float64
	var count int
	w := DefaultWeights()

	for i, e := range events {
		if e.Type == EventAppOpen {
			// Find next open event
			var nextOpen *Event
			for j := i + 1; j < len(events); j++ {
				if events[j].Type == EventOpen || events[j].Type == EventSuggestionClick {
					nextOpen = &events[j]
					break
				}
			}
			if nextOpen != nil {
				count++
				prof := BuildProfile(events[:i], e.Time) // Ignoring embeddings for fast offline replay
				preds := PredictWithWeights(files, prof, w, e.Time, 5)

				rank := 0
				for r, p := range preds {
					if p.Path == nextOpen.Path {
						rank = r + 1
						break
					}
				}

				if rank > 0 {
					hits++
					sumRR += 1.0 / float64(rank)
				}

				// Learn! Calculate f- (negatives) based on position bias.
				// Any suggestion that was ranked ABOVE the clicked file is a negative sample.
				// If not in top 5, all top 5 are negative samples.
				var negFiles []FileCandidate
				limit := rank - 1
				if rank == 0 {
					limit = len(preds)
				}
				for r := 0; r < limit; r++ {
					for _, fc := range files {
						if fc.Path == preds[r].Path {
							negFiles = append(negFiles, fc)
							break
						}
					}
				}

				var posFile FileCandidate
				foundPos := false
				for _, fc := range files {
					if fc.Path == nextOpen.Path {
						posFile = fc
						foundPos = true
						break
					}
				}

				// Only learn if posFile exists
				if foundPos && len(negFiles) > 0 {
					w = Learn(posFile, negFiles, prof, w, nextOpen.Time)
				}
			}
		}
	}

	if count == 0 {
		return 0, 0
	}
	return float64(hits) / float64(count), sumRR / float64(count)
}

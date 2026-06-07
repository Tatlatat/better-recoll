package intent

import (
	"time"
)

// Learning parameters
const (
	learningRate = 0.02
	margin       = 0.05
	minWeight    = 0.05
)

// Learn updates weights using Passive-Aggressive algorithm for pairs of (positive, negatives).
// It returns the updated Weights.
func Learn(posFile FileCandidate, negFiles []FileCandidate, prof Profile, currentWeights Weights, now time.Time) Weights {
	w := currentWeights

	// Calculate features for positive
	posRec, posFreq, posCos, posTime := CalculateFeatures(posFile, prof, now)
	posScore := w.Rec*posRec + w.Freq*posFreq + w.Cos*posCos + w.Time*posTime

	for _, negFile := range negFiles {
		negRec, negFreq, negCos, negTime := CalculateFeatures(negFile, prof, now)
		negScore := w.Rec*negRec + w.Freq*negFreq + w.Cos*negCos + w.Time*negTime

		if posScore < negScore+margin {
			// Update weights
			e := margin - (posScore - negScore)
			
			w.Rec += learningRate * e * (posRec - negRec)
			w.Freq += learningRate * e * (posFreq - negFreq)
			w.Cos += learningRate * e * (posCos - negCos)
			w.Time += learningRate * e * (posTime - negTime)
			
			// Clamp weights
			if w.Rec < minWeight { w.Rec = minWeight }
			if w.Freq < minWeight { w.Freq = minWeight }
			if w.Cos < minWeight { w.Cos = minWeight }
			if w.Time < minWeight { w.Time = minWeight }
			
			// Normalize
			sum := w.Rec + w.Freq + w.Cos + w.Time
			w.Rec /= sum
			w.Freq /= sum
			w.Cos /= sum
			w.Time /= sum
			
			// Recalculate posScore for next negative comparison
			posScore = w.Rec*posRec + w.Freq*posFreq + w.Cos*posCos + w.Time*posTime
		}
	}

	return w
}

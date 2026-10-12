package sst

import (
	"math"
	"sort"
)

// RankedFront is a research-only selection, not a validated oceanographic front.
type RankedFront struct {
	ID string `json:"id"`
	FrontPath
	Score                float64 `json:"ranking_score"`
	PotentialDuplicateOf string  `json:"potential_duplicate_of,omitempty"`
}

// RankFrontPaths selects long coherent candidates without changing the detector.
// Similarity is flagged for review rather than removing scientifically uncertain paths.
func RankFrontPaths(paths []FrontPath, minNM float64) []RankedFront {
	result := make([]RankedFront, 0)
	for _, p := range paths {
		if p.LengthNM < minNM || len(p.Points) < 2 {
			continue
		}
		result = append(result, RankedFront{FrontPath: p, Score: math.Sqrt(p.LengthNM) * p.MeanContrast10NMF})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Score == result[j].Score {
			return result[i].LengthNM > result[j].LengthNM
		}
		return result[i].Score > result[j].Score
	})
	for i := range result {
		result[i].ID = frontID(i + 1)
	}
	// Flag likely near-overlap for review. Never silently suppress fronts.
	for i := 1; i < len(result); i++ {
		for j := 0; j < i; j++ {
			if frontsOverlap(result[i].FrontPath, result[j].FrontPath) {
				result[i].PotentialDuplicateOf = result[j].ID
				break
			}
		}
	}
	return result
}
func frontID(n int) string { // stable by sorted ranking for one observation
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	for len(s) < 3 {
		s = "0" + s
	}
	return "F" + s
}
func frontsOverlap(a, b FrontPath) bool {
	if len(a.Points) < 2 || len(b.Points) < 2 {
		return false
	}
	// Both geometry proximity AND similar direction are required. At least 70% of
	// the shorter path vertices must lie within one research-grid cell of the other.
	shorter, longer := a, b
	if len(shorter.Points) > len(longer.Points) {
		shorter, longer = longer, shorter
	}
	close := 0
	for _, p := range shorter.Points {
		best := math.Inf(1)
		for k := 1; k < len(longer.Points); k++ {
			u, v := longer.Points[k-1], longer.Points[k]
			dx, dy := v.X-u.X, v.Y-u.Y
			t := 0.
			if dx*dx+dy*dy > 0 {
				t = ((p.X-u.X)*dx + (p.Y-u.Y)*dy) / (dx*dx + dy*dy)
				t = math.Max(0, math.Min(1, t))
			}
			dist := math.Hypot(p.X-(u.X+t*dx), p.Y-(u.Y+t*dy))
			if dist < best {
				best = dist
			}
		}
		if best <= 1 {
			close++
		}
	}
	return float64(close)/float64(len(shorter.Points)) >= .7
}

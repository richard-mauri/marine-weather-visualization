package sst

import (
	"math"
	"testing"
)

func TestCandidateBoundariesRejectRampAndAcceptStep(t *testing.T) {
	mk := func(step bool) [][]float64 {
		g := make([][]float64, 60)
		for y := range g {
			g[y] = make([]float64, 60)
			for x := range g[y] {
				if step {
					if x < 30 {
						g[y][x] = 18
					} else {
						g[y][x] = 21
					}
				} else {
					g[y][x] = 18 + float64(x)*.05
				}
			}
		}
		return g
	}
	win := []FrontWindow{{Row: 8, Col: 8, Size: 44, ThresholdC: 19.5}}
	opts := DefaultBoundaryOptions()
	a, _ := ExtractCandidateBoundaries(mk(true), win, 32, .03, opts)
	b, _ := ExtractCandidateBoundaries(mk(false), win, 32, .03, opts)
	if len(a) == 0 {
		t.Fatal("sharp front not detected")
	}
	if len(b) != 0 {
		t.Fatalf("diffuse ramp returned %d segments", len(b))
	}
}
func TestCandidateBoundariesIgnoreMissing(t *testing.T) {
	g := make([][]float64, 50)
	for y := range g {
		g[y] = make([]float64, 50)
		for x := range g[y] {
			g[y][x] = math.NaN()
		}
	}
	s, _ := ExtractCandidateBoundaries(g, []FrontWindow{{Row: 0, Col: 0, Size: 32, ThresholdC: 20}}, 30, .03, DefaultBoundaryOptions())
	if len(s) > 0 {
		t.Fatal("missing values became fronts")
	}
}

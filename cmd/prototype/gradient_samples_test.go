package main

import (
	"github.com/richard-mauri/marine-weather-visualization/geo"
	"math"
	"testing"
)

func TestNumericalGradientSamples(t *testing.T) {
	m := make([][]float64, 81)
	for r := range m {
		m[r] = make([]float64, 81)
		for c := range m[r] {
			m[r][c] = 20 + float64(c)*.04
		}
	}
	b := geo.Bounds{West: -124, East: -123.2, South: 32, North: 32.8}
	v := numericalGradientSamples(m, b, b)
	if len(v) == 0 {
		t.Fatal("expected measurable numerical SST gradient")
	}
	for _, p := range v {
		if len(p) != 3 || math.IsNaN(p[2]) || p[2] <= 0 {
			t.Fatalf("bad sample: %v", p)
		}
	}
	for r := range m {
		for c := range m[r] {
			m[r][c] = 20
		}
	}
	if got := numericalGradientSamples(m, b, b); len(got) != 0 {
		t.Fatalf("uniform water has %d gradient samples", len(got))
	}
}

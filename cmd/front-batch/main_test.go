package main

import (
	"math"
	"testing"
)

func TestProfile(t *testing.T) {
	v := make([]float64, 21)
	ok := make([]bool, 21)
	for i := range v {
		ok[i] = true
		v[i] = 20
		if i >= 10 {
			v[i] = 22
		}
	}
	r := analyze(v, ok, 3.6)
	if r.Status != "ok" || math.Abs(r.Measured-3.6) > 1e-8 || math.Abs(r.PeakOffset) > 1 {
		t.Fatalf("unexpected %+v", r)
	}
}
func TestMissing(t *testing.T) {
	v := make([]float64, 21)
	ok := make([]bool, 21)
	r := analyze(v, ok, 0)
	if r.Status != "insufficient_data" {
		t.Fatal(r.Status)
	}
}

func TestTileCoverageAndSize(t *testing.T) {
	// 20 NM extends 10 NM from the center in either normal direction.
	for _, lat := range []float64{26.01, 27.49, 29.99, 31.75, 33.99} {
		for _, lon := range []float64{-121.99, -119.51, -117.49, -115.01} {
			b := tileBounds(lat, lon)
			if math.Abs((b.East-b.West)-1) > 1e-10 || math.Abs((b.North-b.South)-1) > 1e-10 {
				t.Fatalf("invalid tile: %+v", b)
			}
			for angle := 0; angle < 360; angle += 10 {
				r := float64(angle) * math.Pi / 180
				x := lon + math.Cos(r)*10/(60*math.Cos(lat*math.Pi/180))
				y := lat + math.Sin(r)*10/60
				if x < b.West || x > b.East || y < b.South || y > b.North {
					t.Fatalf("transect endpoint outside tile: (%f,%f) %+v", lat, lon, b)
				}
			}
		}
	}
}

func TestTransectAlongUnequalSegments(t *testing.T) {
	coords := [][]float64{{-117, 30}, {-117, 30.1}, {-117, 30.5}}
	a, ok := transectAt(coords, 0.25)
	if !ok || math.Abs(a.Lat-30.125) > 1e-7 || math.Abs(a.East+1) > 1e-8 || math.Abs(a.North) > 1e-8 {
		t.Fatalf("quarter %+v %v", a, ok)
	}
	b, ok := transectAt(coords, 0.75)
	if !ok || math.Abs(b.Lat-30.375) > 1e-7 {
		t.Fatalf("three-quarter %+v %v", b, ok)
	}
	if _, ok := transectAt([][]float64{{-117, 30}, {-117, 30}}, 0.5); ok {
		t.Fatal("degenerate path accepted")
	}
}

func TestFrontWidthAndCentralConcentration(t *testing.T) {
	v := make([]float64, 21)
	ok := make([]bool, 21)
	for i := range ok {
		ok[i] = true
	}
	// Three adjacent high-slope intervals around front (indices 8..10).
	for i := 1; i < 21; i++ {
		v[i] = v[i-1]
		if i >= 9 && i <= 11 {
			v[i] += 1
		}
	}
	r := analyze(v, ok, 5.4)
	if r.Status != "ok" || r.WidthNM != 3 || math.Abs(r.CentralFraction-1) > 1e-9 || r.PeakFlag != "near_center" {
		t.Fatalf("metrics: %+v", r)
	}
}
func TestDisplacedPeakDiagnostic(t *testing.T) {
	v := make([]float64, 21)
	ok := make([]bool, 21)
	for i := range ok {
		ok[i] = true
	}
	for i := 1; i < 21; i++ {
		v[i] = v[i-1]
		if i == 20 {
			v[i] += 3
		}
		if i == 10 {
			v[i] += 1
		}
	}
	r := analyze(v, ok, 1.8)
	if r.PeakFlag != "displaced_peak" || r.PeakOffset != 9.5 {
		t.Fatalf("diagnostic: %+v", r)
	}
}

func TestQualityScreen(t *testing.T) {
	cases := []struct {
		valid, total, competing, displaced int
		want                               string
	}{
		{0, 3, 0, 0, "no_usable_transects"},
		{3, 3, 0, 1, "review_displaced_peak"},
		{3, 3, 1, 0, "review_competing_gradient"},
		{2, 3, 0, 0, "partial_sampling"},
		{3, 3, 0, 0, "no_diagnostic_flags"},
	}
	for _, c := range cases {
		if got := qualityFlag(c.valid, c.total, c.competing, c.displaced); got != c.want {
			t.Fatalf("got %s want %s", got, c.want)
		}
	}
}

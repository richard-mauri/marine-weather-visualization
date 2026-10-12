package sst

import (
	"math"
	"testing"
)

func TestWindowDetectsCoherentWaterMasses(t *testing.T) {
	g := make([][]float64, 48)
	for y := range g {
		g[y] = make([]float64, 48)
		for x := range g[y] {
			if x < 24 {
				g[y][x] = 12
			} else {
				g[y][x] = 16
			}
		}
	}
	found := FindFrontWindows(g, DefaultWindowFrontOptions())
	if len(found) == 0 {
		t.Fatal("expected coherent warm/cold boundary")
	}
	for _, f := range found {
		if f.ContrastC < 3.9 || f.SpatialCoherence < .99 {
			t.Fatalf("unexpected front stats: %+v", f)
		}
	}
}
func TestWindowRejectsUniformField(t *testing.T) {
	g := make([][]float64, 32)
	for y := range g {
		g[y] = make([]float64, 32)
		for x := range g[y] {
			g[y][x] = 13
		}
	}
	if n := len(FindFrontWindows(g, DefaultWindowFrontOptions())); n != 0 {
		t.Fatalf("uniform field: %d fronts", n)
	}
}
func TestWindowRejectsCheckerboard(t *testing.T) {
	g := make([][]float64, 32)
	for y := range g {
		g[y] = make([]float64, 32)
		for x := range g[y] {
			if (x+y)%2 == 0 {
				g[y][x] = 12
			} else {
				g[y][x] = 16
			}
		}
	}
	if n := len(FindFrontWindows(g, DefaultWindowFrontOptions())); n != 0 {
		t.Fatalf("checkerboard: %d fronts", n)
	}
}
func TestWindowRejectsMissingData(t *testing.T) {
	g := make([][]float64, 32)
	for y := range g {
		g[y] = make([]float64, 32)
		for x := range g[y] {
			g[y][x] = math.NaN()
		}
	}
	if n := len(FindFrontWindows(g, DefaultWindowFrontOptions())); n != 0 {
		t.Fatalf("missing-data field: %d fronts", n)
	}
}

package main

import (
	"errors"
	"testing"
)

func TestN28ClassifyTileFailures(t *testing.T) {
	for _, tc := range []struct{ msg, expected string }{
		{"SST not cached; this browser is in Cache Only mode", "not_cached"},
		{"ERDDAP temporarily unavailable; retry after 2s", "noaa_unavailable"},
		{"context canceled", "request_interrupted"},
		{"upstream HTTP 503", "retrieval_failed"},
	} {
		if got := classifyFrontTileFailure(errors.New(tc.msg)); got != tc.expected {
			t.Errorf("%q => %q, expected %q", tc.msg, got, tc.expected)
		}
	}
}
func TestN28DistinctCurvedTracksSameEndpointsRemain(t *testing.T) {
	mk := func(c [][]float64) detectedFeature {
		f := detectedFeature{Type: "Feature"}
		f.Geometry.Type = "LineString"
		f.Geometry.Coordinates = c
		return f
	}
	a := mk([][]float64{{-120, 30}, {-119.95, 30.02}, {-119.9, 30}})
	b := mk([][]float64{{-120, 30}, {-119.95, 29.98}, {-119.9, 30}})
	reversed := mk([][]float64{{-119.9, 30}, {-119.95, 30.02}, {-120, 30}})
	result := dedupeTiledFeatures([]detectedFeature{a, b, reversed})
	if len(result) != 2 {
		t.Fatalf("expected distinct arcs but not reverse duplicate, got %d", len(result))
	}
}

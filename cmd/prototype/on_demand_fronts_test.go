package main

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/richard-mauri/marine-weather-visualization/geo"
	"github.com/richard-mauri/marine-weather-visualization/sst"
)

func TestOnDemandRejectsLargeViewport(t *testing.T) {
	store := newGridStore(func(context.Context, geo.Bounds) (sst.Grid, error) { t.Fatal("must not fetch"); return sst.Grid{}, nil })
	r := httptest.NewRequest("GET", "/api/detect-fronts?date=2026-10-02&west=-125&south=30&east=-121&north=34", nil)
	w := httptest.NewRecorder()
	newFrontRequestCache().serve(store)(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
func TestOnDemandFlatGridProducesNoInventedLines(t *testing.T) {
	store := newGridStore(func(_ context.Context, b geo.Bounds) (sst.Grid, error) {
		g := sst.Grid{Timestamp: "2026-10-02T09:00:00Z"}
		for y := 0; y <= 60; y++ {
			for x := 0; x <= 60; x++ {
				g.Points = append(g.Points, sst.Point{Lat: 31 - float64(y)*.01, Lon: -119 + float64(x)*.01, Temp: 20 + math.Sin(float64(x)*.001)})
			}
		}
		return g, nil
	})
	r := httptest.NewRequest("GET", "/api/detect-fronts?date=2026-10-02&west=-119&south=30.4&east=-118.4&north=31", nil)
	w := httptest.NewRecorder()
	newFrontRequestCache().serve(store)(w, r)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"features":[]`) {
		t.Fatalf("expected empty candidates, got %s", w.Body.String()[:min(300, len(w.Body.String()))])
	}
}

func TestClipFrontPolylineSelectedArea(t *testing.T) {
	b := geo.Bounds{West: -119, East: -118.4, South: 32, North: 32.6}
	paths := clipFrontPolyline([][]float64{{-119.2, 32.3}, {-118.7, 32.3}, {-118.2, 32.3}}, b)
	if len(paths) != 1 || len(paths[0]) < 2 {
		t.Fatalf("expected one clipped path, got %v", paths)
	}
	if math.Abs(paths[0][0][0]-b.West) > 1e-8 || math.Abs(paths[0][len(paths[0])-1][0]-b.East) > 1e-8 {
		t.Fatalf("incorrect endpoints: %v", paths)
	}
}
func TestClipFrontPolylineOutside(t *testing.T) {
	b := geo.Bounds{West: -119, East: -118.4, South: 32, North: 32.6}
	if parts := clipFrontPolyline([][]float64{{-120, 33}, {-119.5, 33}}, b); len(parts) != 0 {
		t.Fatalf("expected no paths, got %v", parts)
	}
}

func TestN2DiskFrontCacheAndBounds(t *testing.T) {
	t.Setenv("FRONT_CACHE_DIR", t.TempDir())
	key := "n2-2026-10-02-demo"
	initial := detectedResponse{Type: "FeatureCollection", AnalysisDate: "2026-10-02", Features: []detectedFeature{}, TilesTotal: 25, TilesCompleted: 25}
	writeFrontDisk(key, initial)
	got, ok := readFrontDisk(key)
	if !ok || got.TilesTotal != 25 || got.TilesCompleted != 25 {
		t.Fatalf("persistent front cache round trip failed: %+v %v", got, ok)
	}
	b := geo.Bounds{West: -122, East: -119, South: 30, North: 33}
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestN2PolylineDedupeAndNM(t *testing.T) {
	mk := func(c [][]float64) detectedFeature {
		f := detectedFeature{Type: "Feature"}
		f.Geometry.Type = "LineString"
		f.Geometry.Coordinates = c
		return f
	}
	a := mk([][]float64{{-120, 30}, {-119.9, 30}})
	b := mk([][]float64{{-119.9, 30}, {-120, 30}})
	if len(dedupeTiledFeatures([]detectedFeature{a, b})) != 1 {
		t.Fatal("reversed duplicate survived")
	}
	if polylineNM(a.Geometry.Coordinates) < 4 {
		t.Fatal("incorrect nautical-mile length")
	}
}

package main

import "testing"

func TestTilePyramidAndWorldEdges(t *testing.T) {
	for _, span := range []int{1, 2, 4, 8, 16, 32, 64, 128} {
		if !validTileSpan(span) {
			t.Errorf("tile span %d incorrectly rejected", span)
		}
	}
	if validTileSpan(3) || validTileSpan(256) {
		t.Fatal("unsupported span accepted")
	}
	for _, tc := range []struct {
		span, x, y               int
		west, east, south, north float64
		ok                       bool
	}{
		{128, -2, 0, -180, -128, 0, 85, true},
		{128, 1, -1, 128, 180, -85, 0, true},
		{128, 0, 0, 0, 128, 0, 85, true},
		{64, -1, -2, -64, 0, -85, -64, true},
		{16, -1, 5, -16, 0, 80, 85, true},
		{16, -1, -6, -16, 0, -85, -80, true},
		{16, -1, 6, 0, 0, 0, 0, false},
		{64, 0, 2, 0, 0, 0, 0, false},
		{128, 2, 0, 0, 0, 0, 0, false},
	} {
		b, ok := clippedTileBounds(tc.span, tc.x, tc.y)
		if ok != tc.ok {
			t.Errorf("%+v validity %v", tc, ok)
			continue
		}
		if ok && (b.West != tc.west || b.East != tc.east || b.South != tc.south || b.North != tc.north) {
			t.Errorf("%+v: got %+v", tc, b)
		}
	}
}

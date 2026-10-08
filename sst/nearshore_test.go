package sst

import (
	"github.com/richard-mauri/marine-weather-visualization/geo"
	"image"
	"image/color"
	"testing"
)

func TestNearshoreFillRespectsLandAndNoData(t *testing.T) {
	raw := []byte(`{"type":"FeatureCollection","features":[{"geometry":{"type":"Polygon","coordinates":[[[0.045,0],[0.055,0],[0.055,0.1],[0.045,0.1],[0.045,0]]]}}]}`)
	land, err := ParseLandGeoJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	im := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	im.SetNRGBA(42, 50, color.NRGBA{100, 120, 140, 220})
	b := geo.Bounds{West: 0, East: .1, South: 0, North: .1}
	n := FillNearshore(im, b, land)
	if n == 0 {
		t.Fatal("did not fill water adjacent to valid data")
	}
	if got := im.NRGBAAt(43, 50).A; got != 150 {
		t.Fatalf("estimated opacity=%d", got)
	}
	if got := im.NRGBAAt(50, 50).A; got != 0 {
		t.Fatalf("land filled: %d", got)
	}
	if got := im.NRGBAAt(90, 50).A; got != 0 {
		t.Fatalf("far no-data filled: %d", got)
	}
	if got := im.NRGBAAt(42, 50).A; got != 220 {
		t.Fatalf("original changed: %d", got)
	}
}
func TestNearshoreFillDisabledWithoutLandMask(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	im.SetNRGBA(4, 4, color.NRGBA{1, 2, 3, 220})
	if n := FillNearshore(im, geo.Bounds{West: 0, East: .1, South: 0, North: .1}, nil); n != 0 {
		t.Fatal(n)
	}
}

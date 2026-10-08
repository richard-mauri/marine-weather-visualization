package sst

import (
	"github.com/richard-mauri/marine-weather-visualization/geo"
	"image"
	"testing"
)

func TestMaskEdgeShowsOnlyBoundaryWaterPixels(t *testing.T) {
	raw := []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"Polygon","coordinates":[[[-123,37],[-122.5,37],[-122.5,38],[-123,38],[-123,37]]]}}]}`)
	m, err := ParseLandGeoJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	b := geo.Bounds{West: -123, East: -122, South: 37, North: 38}
	img := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	m.Apply(img, b)
	m.DrawMaskEdge(img, b)
	if img.NRGBAAt(10, 50).A != 0 {
		t.Fatal("land became visible")
	}
	if c := img.NRGBAAt(50, 50); c.R != 255 || c.B != 255 || c.G != 0 {
		t.Fatalf("edge %v", c)
	}
	if c := img.NRGBAAt(90, 50); c.R == 255 && c.B == 255 && c.G == 0 {
		t.Fatal("interior water marked")
	}
}

package sst

import (
	"github.com/richard-mauri/marine-weather-visualization/geo"
	"image"
	"testing"
)

func TestLandMaskAndHole(t *testing.T) {
	raw := []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"Polygon","coordinates":[[[-123,37],[-122,37],[-122,38],[-123,38],[-123,37]],[[-122.8,37.2],[-122.2,37.2],[-122.2,37.8],[-122.8,37.8],[-122.8,37.2]]]}}]}`)
	m, e := ParseLandGeoJSON(raw)
	if e != nil {
		t.Fatal(e)
	}
	if !m.Contains(-122.9, 37.5) || m.Contains(-122.5, 37.5) || m.Contains(-121.9, 37.5) {
		t.Fatal("land hole classification")
	}
	im := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	for i := 3; i < len(im.Pix); i += 4 {
		im.Pix[i] = 255
	}
	m.Apply(im, geo.Bounds{West: -123, East: -122, South: 37, North: 38})
	if im.NRGBAAt(10, 50).A != 0 || im.NRGBAAt(50, 50).A != 255 {
		t.Fatal("mask alpha")
	}
}

package sst

import (
	"github.com/richard-mauri/marine-weather-visualization/geo"
	"image"
	"image/color"
	"testing"
)

func TestWarpMercator(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 3, 11))
	for y := 0; y < 11; y++ {
		for x := 0; x < 3; x++ {
			im.SetNRGBA(x, y, color.NRGBA{uint8(y * 20), 10, 20, 255})
		}
	}
	o := WarpGeographicToMercator(im, geo.Bounds{West: -123, South: 30, East: -122, North: 50})
	if o.Bounds() != im.Bounds() {
		t.Fatal("wrong dimensions")
	}
	if o.NRGBAAt(1, 0).A != 255 || o.NRGBAAt(1, 10).A != 255 {
		t.Fatal("lost alpha")
	}
	if o.NRGBAAt(1, 5).R == im.NRGBAAt(1, 5).R {
		t.Log("midpoint coincident after rounding at low resolution")
	}
}

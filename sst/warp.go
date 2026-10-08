package sst

import (
	"github.com/richard-mauri/marine-weather-visualization/geo"
	"image"
	"image/color"
	"math"
)

// WarpGeographicToMercator vertically resamples an EPSG:4326 image to the
// Web Mercator y-coordinate that Leaflet imageOverlay interpolates linearly.
// Source image pixels are evenly spaced in latitude; target pixels evenly
// spaced in Mercator y. Out-of-range source samples are transparent.
func WarpGeographicToMercator(src image.Image, b geo.Bounds) *image.NRGBA {
	sb := src.Bounds()
	width, height := sb.Dx(), sb.Dy()
	out := image.NewNRGBA(image.Rect(0, 0, width, height))
	if width == 0 || height == 0 || b.North <= b.South {
		return out
	}
	top := mercator(b.North)
	bottom := mercator(b.South)
	for y := 0; y < height; y++ {
		fraction := (float64(y) + 0.5) / float64(height)
		lat := inverseMercator(top + (bottom-top)*fraction)
		srcY := (b.North-lat)/(b.North-b.South)*float64(height) - 0.5
		iy := int(math.Round(srcY))
		if iy < 0 {
			iy = 0
		}
		if iy >= height {
			iy = height - 1
		}
		for x := 0; x < width; x++ {
			c := color.NRGBAModel.Convert(src.At(sb.Min.X+x, sb.Min.Y+iy)).(color.NRGBA)
			out.SetNRGBA(x, y, c)
		}
	}
	return out
}
func mercator(lat float64) float64 {
	r := lat * math.Pi / 180
	return math.Log(math.Tan(math.Pi/4 + r/2))
}
func inverseMercator(y float64) float64 {
	return (2*math.Atan(math.Exp(y)) - math.Pi/2) * 180 / math.Pi
}

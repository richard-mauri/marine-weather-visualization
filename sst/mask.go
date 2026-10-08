package sst

import (
	"image"
	"image/color"
)

// MaskLand removes SST pixels wherever the transparent WMS Land layer is opaque.
// Both images MUST come from the same bounding box and projection.
// Threshold 32 discards antialiased shoreline pixels as well as solid land.
func MaskLand(sea image.Image, land image.Image) *image.NRGBA {
	b := sea.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	lb := land.Bounds()
	if lb.Dx() != b.Dx() || lb.Dy() != b.Dy() {
		return out
	}
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			c := color.NRGBAModel.Convert(sea.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			_, _, _, a := land.At(lb.Min.X+x, lb.Min.Y+y).RGBA()
			if a > 32*257 {
				c.A = 0
			}
			out.SetNRGBA(x, y, c)
		}
	}
	return out
}

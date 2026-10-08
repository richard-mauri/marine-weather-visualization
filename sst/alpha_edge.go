package sst

import "image"

// DrawAlphaEdge traces the final composed PNG alpha, not a second independent
// polygon rasterization. It leaves RGB temperature samples untouched except at
// the visible diagnostic line. Source no-data borders are also traced.
// One output pixel on the opaque side of any interior transparent neighbor.
func DrawAlphaEdge(img *image.NRGBA) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 3 || h < 3 {
		return
	}
	opaque := make([]bool, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			opaque[y*w+x] = img.Pix[img.PixOffset(b.Min.X+x, b.Min.Y+y)+3] != 0
		}
	}
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			i := y*w + x
			if opaque[i] && (!opaque[i-1] || !opaque[i+1] || !opaque[i-w] || !opaque[i+w]) {
				o := img.PixOffset(b.Min.X+x, b.Min.Y+y)
				img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = 255, 0, 255, 255
			}
		}
	}
}

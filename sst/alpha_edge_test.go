package sst

import (
	"image"
	"image/color"
	"testing"
)

func TestDrawAlphaEdgeUsesFinalAlpha(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 9, 9))
	for y := 1; y < 8; y++ {
		for x := 4; x < 8; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 100, G: 150, B: 180, A: 255})
		}
	}
	DrawAlphaEdge(img)
	if got := img.NRGBAAt(4, 4); got != (color.NRGBA{R: 255, G: 0, B: 255, A: 255}) {
		t.Fatalf("boundary=%v", got)
	}
	if got := img.NRGBAAt(6, 4); got != (color.NRGBA{R: 100, G: 150, B: 180, A: 255}) {
		t.Fatalf("interior changed=%v", got)
	}
	if got := img.NRGBAAt(3, 4); got.A != 0 {
		t.Fatalf("transparent area changed=%v", got)
	}
}

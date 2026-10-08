package sst

import (
	"image"
	"image/color"
	"testing"
)

func TestMaskLand(t *testing.T) {
	s := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	l := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	s.SetNRGBA(0, 0, color.NRGBA{0, 200, 0, 255})
	s.SetNRGBA(1, 0, color.NRGBA{0, 200, 0, 255})
	l.SetNRGBA(1, 0, color.NRGBA{255, 255, 255, 255})
	o := MaskLand(s, l)
	if o.NRGBAAt(0, 0).A != 255 || o.NRGBAAt(1, 0).A != 0 {
		t.Fatal("mask failed")
	}
}

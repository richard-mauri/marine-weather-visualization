package sst

import (
	"github.com/richard-mauri/marine-weather-visualization/geo"
	"image"
	"math"
)

// FillNearshore estimates temperatures only a short distance from existing valid
// raster pixels. Each estimate inherits the color of its nearest original source
// pixel, with reduced alpha (150 vs 220) to distinguish it from measured coverage.
// The land mask is mandatory; no pixel in a land polygon is filled.
// The source grid is NOT modified. Returns the number of estimated pixels.
func FillNearshore(img *image.NRGBA, b geo.Bounds, land *LandMask) int {
	if land == nil {
		return 0
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if w < 2 || h < 2 {
		return 0
	}
	// Never bridge more than ~2 km in either direction; this also avoids
	// inventing wide ocean areas and keeps coarse global views unchanged.
	const maxDegrees = 0.018
	pixelsX := int(math.Ceil(maxDegrees / ((b.East - b.West) / float64(w))))
	pixelsY := int(math.Ceil(maxDegrees / ((b.North - b.South) / float64(h))))
	if pixelsX < 1 || pixelsY < 1 {
		return 0
	}
	if pixelsX > 24 {
		pixelsX = 24
	}
	if pixelsY > 24 {
		pixelsY = 24
	}
	type node struct{ x, y, ox, oy int }
	visited := make([]bool, w*h)
	queue := make([]node, 0, w*h)
	// Only original valid samples are sources; estimates are never chained beyond
	// the strict source-to-target geographic-distance test.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if img.Pix[img.PixOffset(x, y)+3] > 0 {
				i := y*w + x
				visited[i] = true
				queue = append(queue, node{x, y, x, y})
			}
		}
	}
	if len(queue) == 0 {
		return 0
	}
	north, south := merc(b.North), merc(b.South)
	count := 0
	for head := 0; head < len(queue); head++ {
		p := queue[head]
		for _, d := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			x, y := p.x+d[0], p.y+d[1]
			if x < 0 || x >= w || y < 0 || y >= h {
				continue
			}
			i := y*w + x
			if visited[i] {
				continue
			}
			if abs(x-p.ox) > pixelsX || abs(y-p.oy) > pixelsY {
				continue
			}
			lon := b.West + (float64(x)+.5)/float64(w)*(b.East-b.West)
			v := north - (float64(y)+.5)/float64(h)*(north-south)
			lat := (2*math.Atan(math.Exp(v)) - math.Pi/2) * 180 / math.Pi
			// Check physical displacement, not just a rounded pixel count.
			sv := north - (float64(p.oy)+.5)/float64(h)*(north-south)
			slat := (2*math.Atan(math.Exp(sv)) - math.Pi/2) * 180 / math.Pi
			if math.Abs(lat-slat) > maxDegrees || math.Abs(float64(x-p.ox))*(b.East-b.West)/float64(w) > maxDegrees {
				continue
			}
			if land.Contains(lon, lat) {
				continue
			}
			visited[i] = true
			source := img.PixOffset(p.ox, p.oy)
			target := img.PixOffset(x, y)
			copy(img.Pix[target:target+4], img.Pix[source:source+4])
			img.Pix[target+3] = 150
			queue = append(queue, node{x, y, p.ox, p.oy})
			count++
		}
	}
	return count
}
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

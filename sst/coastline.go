package sst

import (
	"encoding/json"
	"errors"
	"github.com/richard-mauri/marine-weather-visualization/geo"
	"image"
	"math"
)

// LandMask clips rendered pixels to land polygons supplied by a trusted local
// GeoJSON file (WGS84 lon/lat). It does NOT increase source data resolution.
type LandMask struct{ polygons []landPolygon }
type landPolygon struct {
	rings                    [][][]float64
	west, east, south, north float64
}

func ParseLandGeoJSON(raw []byte) (*LandMask, error) {
	var fc struct {
		Type     string `json:"type"`
		Features []struct {
			Geometry json.RawMessage `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(raw, &fc); err != nil {
		return nil, err
	}
	if fc.Type != "FeatureCollection" {
		return nil, errors.New("expected GeoJSON FeatureCollection")
	}
	m := &LandMask{}
	for _, f := range fc.Features {
		var g struct {
			Type        string          `json:"type"`
			Coordinates json.RawMessage `json:"coordinates"`
		}
		if json.Unmarshal(f.Geometry, &g) != nil {
			continue
		}
		var polys [][][][]float64
		switch g.Type {
		case "Polygon":
			var rings [][][]float64
			if json.Unmarshal(g.Coordinates, &rings) == nil {
				polys = append(polys, rings)
			}
		case "MultiPolygon":
			_ = json.Unmarshal(g.Coordinates, &polys)
		}
		for _, rings := range polys {
			p := landPolygon{rings: rings, west: math.Inf(1), east: math.Inf(-1), south: math.Inf(1), north: math.Inf(-1)}
			for _, ring := range rings {
				for _, v := range ring {
					if len(v) != 2 {
						continue
					}
					p.west = math.Min(p.west, v[0])
					p.east = math.Max(p.east, v[0])
					p.south = math.Min(p.south, v[1])
					p.north = math.Max(p.north, v[1])
				}
			}
			if len(rings) > 0 && !math.IsInf(p.west, 0) {
				m.polygons = append(m.polygons, p)
			}
		}
	}
	if len(m.polygons) == 0 {
		return nil, errors.New("no land polygons found")
	}
	return m, nil
}

func ringContains(ring [][]float64, lon, lat float64) bool {
	inside := false
	for i, j := 0, len(ring)-1; i < len(ring); j, i = i, i+1 {
		a, b := ring[i], ring[j]
		if len(a) < 2 || len(b) < 2 {
			continue
		}
		if (a[1] > lat) != (b[1] > lat) && lon < (b[0]-a[0])*(lat-a[1])/(b[1]-a[1])+a[0] {
			inside = !inside
		}
	}
	return inside
}
func (m *LandMask) Contains(lon, lat float64) bool {
	if m == nil {
		return false
	}
	for _, p := range m.polygons {
		if lon < p.west || lon > p.east || lat < p.south || lat > p.north || len(p.rings) == 0 {
			continue
		}
		if !ringContains(p.rings[0], lon, lat) {
			continue
		}
		hole := false
		for _, r := range p.rings[1:] {
			if ringContains(r, lon, lat) {
				hole = true
				break
			}
		}
		if !hole {
			return true
		}
	}
	return false
}
func (m *LandMask) Apply(img *image.NRGBA, b geo.Bounds) {
	if m == nil {
		return
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if w == 0 || h == 0 {
		return
	}
	merc := func(lat float64) float64 { r := lat * math.Pi / 180; return math.Log(math.Tan(math.Pi/4 + r/2)) }
	north, south := merc(b.North), merc(b.South)
	for y := 0; y < h; y++ {
		v := north - (float64(y)+.5)/float64(h)*(north-south)
		lat := (2*math.Atan(math.Exp(v)) - math.Pi/2) * 180 / math.Pi
		for x := 0; x < w; x++ {
			off := img.PixOffset(x, y)
			if img.Pix[off+3] == 0 {
				continue
			}
			lon := b.West + (float64(x)+.5)/float64(w)*(b.East-b.West)
			if m.Contains(lon, lat) {
				img.Pix[off+3] = 0
			}
		}
	}
}

// DrawMaskEdge shows the ACTUAL pixel-center land/water classification used by
// Apply. Magenta pixels mark the water side of a land boundary, for diagnostics.
// It does not modify temperature values outside the diagnostic image.
func (m *LandMask) DrawMaskEdge(img *image.NRGBA, b geo.Bounds) {
	if m == nil {
		return
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if w < 2 || h < 2 {
		return
	}
	north, south := mercatorLatitude(b.North), mercatorLatitude(b.South)
	lats := make([]float64, h)
	for y := range lats {
		v := north - (float64(y)+.5)/float64(h)*(north-south)
		lats[y] = (2*math.Atan(math.Exp(v)) - math.Pi/2) * 180 / math.Pi
	}
	land := make([]bool, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			lon := b.West + (float64(x)+.5)/float64(w)*(b.East-b.West)
			land[y*w+x] = m.Contains(lon, lats[y])
		}
	}
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			i := y*w + x
			if !land[i] && (land[i-1] || land[i+1] || land[i-w] || land[i+w]) {
				off := img.PixOffset(x, y)
				img.Pix[off] = 255
				img.Pix[off+1] = 0
				img.Pix[off+2] = 255
				img.Pix[off+3] = 255
			}
		}
	}
}
func mercatorLatitude(lat float64) float64 {
	r := lat * math.Pi / 180
	return math.Log(math.Tan(math.Pi/4 + r/2))
}

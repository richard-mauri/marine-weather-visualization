package sst

import "math"

// BoundarySegment is a candidate threshold crossing (grid coordinates), not a validated front.
// Contrast10NMF is the estimated Celsius difference across a 10-NM transect, converted to °F.
// Concentration compares the local gradient against the broader cross-front slope.
type BoundarySegment struct {
	X0, Y0, X1, Y1       float64 `json:",omitempty"`
	Contrast10NMF        float64 `json:"contrast_10nm_f"`
	Concentration        float64 `json:"concentration"`
	WindowRow, WindowCol int     `json:"window_row,omitempty"`
}

type BoundaryStats struct {
	RawCrossings    int `json:"raw_crossings"`
	RejectedMissing int `json:"rejected_missing"`
	RejectedWeak    int `json:"rejected_weak"`
	RejectedDiffuse int `json:"rejected_diffuse"`
	Deduplicated    int `json:"deduplicated"`
	Kept            int `json:"kept"`
}

type BoundaryOptions struct{ MinContrast10NMF, MinConcentration float64 }

func DefaultBoundaryOptions() BoundaryOptions {
	return BoundaryOptions{MinContrast10NMF: 1.5, MinConcentration: 1.35}
}
func okTemp(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// ExtractCandidateBoundaries finds iso-threshold crossings in accepted overlapping windows.
// A central transect must have measurable contrast and a concentrated local gradient.
// Sample spacing is geographical; this is exploratory, not a complete Cayula–Cornillon tracer.
func ExtractCandidateBoundaries(g [][]float64, windows []FrontWindow, north, step float64, o BoundaryOptions) ([]BoundarySegment, BoundaryStats) {
	var stats BoundaryStats
	if len(g) < 8 || len(g[0]) < 8 || step <= 0 {
		return nil, stats
	}
	h, w := len(g), len(g[0])
	seen := map[[2]int]bool{}
	out := []BoundarySegment{}
	// Each accepted window supplies a threshold; deduplicate by source grid cell.
	for _, win := range windows {
		for y := win.Row + 2; y < win.Row+win.Size-2 && y < h-3; y++ {
			if y < 3 {
				continue
			}
			for x := win.Col + 2; x < win.Col+win.Size-2 && x < w-3; x++ {
				if x < 3 {
					continue
				}
				a, b, c, d := g[y][x], g[y][x+1], g[y+1][x+1], g[y+1][x]
				if !okTemp(a) || !okTemp(b) || !okTemp(c) || !okTemp(d) {
					stats.RejectedMissing++
					continue
				}
				t := win.ThresholdC
				above := 0
				for _, v := range []float64{a, b, c, d} {
					if v >= t {
						above++
					}
				}
				if above == 0 || above == 4 {
					continue
				}
				stats.RawCrossings++
				key := [2]int{y, x}
				if seen[key] {
					stats.Deduplicated++
					continue
				}
				seen[key] = true
				// Bilinearized crossing endpoints in local cell coordinates.
				type pt struct{ xx, yy float64 }
				var pts []pt
				crossing := func(v0, v1 float64) (float64, bool) {
					if (v0 < t) == (v1 < t) || v0 == v1 {
						return 0, false
					}
					return math.Max(0, math.Min(1, (t-v0)/(v1-v0))), true
				}
				if z, yes := crossing(a, b); yes {
					pts = append(pts, pt{float64(x) + z, float64(y)})
				}
				if z, yes := crossing(b, c); yes {
					pts = append(pts, pt{float64(x + 1), float64(y) + z})
				}
				if z, yes := crossing(d, c); yes {
					pts = append(pts, pt{float64(x) + z, float64(y + 1)})
				}
				if z, yes := crossing(a, d); yes {
					pts = append(pts, pt{float64(x), float64(y) + z})
				}
				if len(pts) < 2 {
					continue
				}
				lat := north - (float64(y)+.5)*step
				dxnm := 60 * step * math.Cos(lat*math.Pi/180)
				dynm := 60 * step
				gx := (g[y][x+1] - g[y][x-1]) / (2 * dxnm)
				gy := (g[y-1][x] - g[y+1][x]) / (2 * dynm)
				if !okTemp(g[y][x-1]) || !okTemp(g[y-1][x]) || !okTemp(g[y+1][x]) {
					stats.RejectedMissing++
					continue
				}
				gm := math.Hypot(gx, gy)
				if gm < 1e-8 {
					stats.RejectedWeak++
					continue
				}
				// Compare local slope with wider, symmetric transect 3 cells each direction.
				nx, ny := gx/gm, gy/gm
				sample := func(dist float64) (float64, bool) {
					xx := float64(x) + .5 + nx*dist/dxnm
					yy := float64(y) + .5 - ny*dist/dynm
					i, j := int(math.Floor(xx)), int(math.Floor(yy))
					if i < 0 || j < 0 || i+1 >= w || j+1 >= h {
						return 0, false
					}
					aa, bb, cc, dd := g[j][i], g[j][i+1], g[j+1][i], g[j+1][i+1]
					if !okTemp(aa) || !okTemp(bb) || !okTemp(cc) || !okTemp(dd) {
						return 0, false
					}
					fx, fy := xx-float64(i), yy-float64(j)
					return aa*(1-fx)*(1-fy) + bb*fx*(1-fy) + cc*fx*fy + dd*(1-fx)*fy, true
				}
				// 5NM each side; distinguish a concentrated change from an approximately linear ramp.
				low, ok1 := sample(-5)
				high, ok2 := sample(5)
				if !ok1 || !ok2 {
					stats.RejectedMissing++
					continue
				}
				across := math.Abs(high-low) * 1.8
				if across < o.MinContrast10NMF {
					stats.RejectedWeak++
					continue
				}
				concentration := gm / (math.Abs(high-low)/10 + 1e-6)
				if concentration < o.MinConcentration {
					stats.RejectedDiffuse++
					continue
				}
				out = append(out, BoundarySegment{X0: pts[0].xx, Y0: pts[0].yy, X1: pts[1].xx, Y1: pts[1].yy, Contrast10NMF: across, Concentration: concentration, WindowRow: win.Row, WindowCol: win.Col})
			}
		}
	}
	stats.Kept = len(out)
	return out, stats
}

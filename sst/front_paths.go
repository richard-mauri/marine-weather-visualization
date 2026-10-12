package sst

import "math"

// FrontPoint is a location in the research grid; latitude/longitude are recovered
// using the report bounds and cell step.
type FrontPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// FrontPath is an exploratory connected sequence of measured boundary segments.
// Contrast is summarized from the constituent measurements, not re-estimated.
type FrontPath struct {
	Points            []FrontPoint `json:"points"`
	LengthNM          float64      `json:"length_nm"`
	MeanContrast10NMF float64      `json:"mean_contrast_10nm_f"`
	MaxContrast10NMF  float64      `json:"max_contrast_10nm_f"`
	Segments          int          `json:"segments"`
}

type FrontPathStats struct {
	InputSegments           int `json:"input_segments"`
	PathsBeforeFilter       int `json:"paths_before_filter"`
	PathsAfterFilter        int `json:"paths_after_filter"`
	RejectedShort           int `json:"rejected_short"`
	RejectedBranchEndpoints int `json:"rejected_branch_endpoints"`
}

func frontDistanceNM(a, b FrontPoint, north, step float64) float64 {
	lat := north - (a.Y+b.Y)*.5*step
	return math.Hypot((b.X-a.X)*step*60*math.Cos(lat*math.Pi/180), (b.Y-a.Y)*step*60)
}

// ConnectFrontPaths connects only coincident segment endpoints. It refuses to
// connect through a junction of degree >2, and never bridges an empty gap.
// Grid positions are rounded to a fine sub-cell precision for numerical noise.
func ConnectFrontPaths(segments []BoundarySegment, north, step, minLengthNM float64) ([]FrontPath, FrontPathStats) {
	stats := FrontPathStats{InputSegments: len(segments)}
	if step <= 0 {
		return nil, stats
	}
	type endpoint struct{ x, y int64 }
	key := func(p FrontPoint) endpoint {
		return endpoint{int64(math.Round(p.X * 1024)), int64(math.Round(p.Y * 1024))}
	}
	type edge struct {
		a, b     FrontPoint
		ka, kb   endpoint
		contrast float64
	}
	edges := make([]edge, 0, len(segments))
	adjacent := map[endpoint][]int{}
	for _, s := range segments {
		a, b := FrontPoint{s.X0, s.Y0}, FrontPoint{s.X1, s.Y1}
		ka, kb := key(a), key(b)
		if ka == kb || frontDistanceNM(a, b, north, step) < 1e-8 {
			continue
		}
		id := len(edges)
		edges = append(edges, edge{a, b, ka, kb, s.Contrast10NMF})
		adjacent[ka] = append(adjacent[ka], id)
		adjacent[kb] = append(adjacent[kb], id)
	}
	for _, es := range adjacent {
		if len(es) > 2 {
			stats.RejectedBranchEndpoints++
		}
	}
	used := make([]bool, len(edges))
	var out []FrontPath
	trace := func(start int, from endpoint) FrontPath {
		e := edges[start]
		p := FrontPath{Points: []FrontPoint{e.a}}
		if from == e.kb {
			p.Points[0] = e.b
		}
		cur := from
		id := start
		for {
			if used[id] {
				break
			}
			used[id] = true
			e = edges[id]
			var next endpoint
			var point FrontPoint
			if cur == e.ka {
				next = e.kb
				point = e.b
			} else {
				next = e.ka
				point = e.a
			}
			p.LengthNM += frontDistanceNM(p.Points[len(p.Points)-1], point, north, step)
			p.Points = append(p.Points, point)
			p.MeanContrast10NMF += e.contrast
			p.MaxContrast10NMF = math.Max(p.MaxContrast10NMF, e.contrast)
			p.Segments++
			// Stop at open endpoints and junctions; a cycle can end at its start.
			if len(adjacent[next]) != 2 || next == from {
				break
			}
			following := -1
			for _, other := range adjacent[next] {
				if !used[other] {
					following = other
					break
				}
			}
			if following < 0 {
				break
			}
			cur = next
			id = following
		}
		if p.Segments > 0 {
			p.MeanContrast10NMF /= float64(p.Segments)
		}
		return p
	}
	for i, e := range edges {
		if used[i] {
			continue
		}
		if len(adjacent[e.ka]) != 2 || len(adjacent[e.kb]) != 2 {
			start := e.ka
			if len(adjacent[start]) == 2 {
				start = e.kb
			}
			p := trace(i, start)
			stats.PathsBeforeFilter++
			if p.LengthNM >= minLengthNM {
				out = append(out, p)
			} else {
				stats.RejectedShort++
			}
		}
	}
	for i, e := range edges {
		if !used[i] {
			p := trace(i, e.ka)
			stats.PathsBeforeFilter++
			if p.LengthNM >= minLengthNM {
				out = append(out, p)
			} else {
				stats.RejectedShort++
			}
		}
	}
	stats.PathsAfterFilter = len(out)
	return out, stats
}

package sst

import (
	"math"
	"sort"
)

// FrontWindow is a *candidate* two-water-mass region, not a validated front line.
// Coordinates here are row/column indices in the source SST matrix.
type FrontWindow struct {
	Row, Col, Size                               int
	ThresholdC, ColdMeanC, WarmMeanC             float64
	ContrastC, SeparationScore, SpatialCoherence float64
	ColdFraction, ValidFraction                  float64
}

// WindowFrontOptions controls the exploratory histogram/cohesion stage of a
// Cayula–Cornillon-inspired front detector. It does not implement the complete
// published multistage contour-following algorithm.
type WindowFrontOptions struct {
	Size, Stride                                                                       int
	MinValidFraction, MinClassFraction, MinContrastC, MinSeparationScore, MinCoherence float64
}

func DefaultWindowFrontOptions() WindowFrontOptions {
	return WindowFrontOptions{Size: 32, Stride: 16, MinValidFraction: .85, MinClassFraction: .20, MinContrastC: .75, MinSeparationScore: .65, MinCoherence: .75}
}

// FindFrontWindows evaluates overlapping windows of a regular SST matrix (°C).
// NaN denotes invalid observations. No nearshore gap-filled values should be
// passed to this function. The caller is responsible for constructing a
// properly georeferenced, sufficiently resolved grid.
func FindFrontWindows(field [][]float64, o WindowFrontOptions) []FrontWindow {
	if len(field) == 0 || o.Size < 4 || o.Stride < 1 || len(field) < o.Size {
		return nil
	}
	width := len(field[0])
	if width < o.Size {
		return nil
	}
	for _, row := range field {
		if len(row) != width {
			return nil
		}
	}
	var result []FrontWindow
	for r := 0; r+o.Size <= len(field); r += o.Stride {
		for c := 0; c+o.Size <= width; c += o.Stride {
			if f, ok := detectWindow(field, r, c, o); ok {
				result = append(result, f)
			}
		}
	}
	return result
}

func detectWindow(g [][]float64, r, c int, o WindowFrontOptions) (FrontWindow, bool) {
	vals := make([]float64, 0, o.Size*o.Size)
	for y := r; y < r+o.Size; y++ {
		for x := c; x < c+o.Size; x++ {
			v := g[y][x]
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				vals = append(vals, v)
			}
		}
	}
	total := o.Size * o.Size
	if float64(len(vals))/float64(total) < o.MinValidFraction || len(vals) < 8 {
		return FrontWindow{}, false
	}
	sort.Float64s(vals)
	// Maximize the between-class variance, with a minimum population size for
	// each class. This is a histogram-separation screening test, not proof of a front.
	n := len(vals)
	prefix := make([]float64, n+1)
	for i, v := range vals {
		prefix[i+1] = prefix[i] + v
	}
	sum := prefix[n]
	globalMean := sum / float64(n)
	totalVar := 0.0
	for _, v := range vals {
		d := v - globalMean
		totalVar += d * d
	}
	if totalVar < 1e-12 {
		return FrontWindow{}, false
	}
	bestScore, bestK := -1.0, -1
	for k := 1; k < n; k++ {
		if vals[k-1] == vals[k] || float64(k)/float64(n) < o.MinClassFraction || float64(n-k)/float64(n) < o.MinClassFraction {
			continue
		}
		coldMean := prefix[k] / float64(k)
		warmMean := (sum - prefix[k]) / float64(n-k)
		score := float64(k*(n-k)) / float64(n) * (warmMean - coldMean) * (warmMean - coldMean) / totalVar
		if score > bestScore {
			bestScore, bestK = score, k
		}
	}
	if bestK < 0 || bestScore < o.MinSeparationScore {
		return FrontWindow{}, false
	}
	coldMean := prefix[bestK] / float64(bestK)
	warmMean := (sum - prefix[bestK]) / float64(n-bestK)
	contrast := warmMean - coldMean
	if contrast < o.MinContrastC {
		return FrontWindow{}, false
	}
	threshold := (vals[bestK-1] + vals[bestK]) / 2
	// Check if each temperature class occupies one dominant 4-connected patch.
	// A checkerboard or noisy mixed field should have low spatial cohesion.
	coherence := spatialCoherence(g, r, c, o.Size, threshold)
	if coherence < o.MinCoherence {
		return FrontWindow{}, false
	}
	return FrontWindow{Row: r, Col: c, Size: o.Size, ThresholdC: threshold, ColdMeanC: coldMean, WarmMeanC: warmMean, ContrastC: contrast, SeparationScore: bestScore, SpatialCoherence: coherence, ColdFraction: float64(bestK) / float64(n), ValidFraction: float64(n) / float64(total)}, true
}

func spatialCoherence(g [][]float64, r, c, size int, threshold float64) float64 {
	labels := make([]int8, size*size)
	totals := [2]int{}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			v := g[r+y][c+x]
			if math.IsNaN(v) || math.IsInf(v, 0) {
				labels[y*size+x] = -1
				continue
			}
			cls := int8(0)
			if v > threshold {
				cls = 1
			}
			labels[y*size+x] = cls
			totals[cls]++
		}
	}
	visited := make([]bool, len(labels))
	maxComponent := [2]int{}
	queue := make([]int, 0, len(labels))
	for i, cls := range labels {
		if cls < 0 || visited[i] {
			continue
		}
		visited[i] = true
		queue = append(queue[:0], i)
		count := 0
		for len(queue) > 0 {
			p := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			count++
			y, x := p/size, p%size
			for _, q := range []int{p - size, p + size, p - 1, p + 1} {
				if q < 0 || q >= len(labels) || visited[q] || labels[q] != cls {
					continue
				}
				if (q == p-1 || q == p+1) && q/size != y {
					continue
				}
				if (q == p-size || q == p+size) && q%size != x {
					continue
				}
				visited[q] = true
				queue = append(queue, q)
			}
		}
		if count > maxComponent[cls] {
			maxComponent[cls] = count
		}
	}
	if totals[0] == 0 || totals[1] == 0 {
		return 0
	}
	a := float64(maxComponent[0]) / float64(totals[0])
	b := float64(maxComponent[1]) / float64(totals[1])
	if a < b {
		return a
	}
	return b
}

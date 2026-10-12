package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/richard-mauri/marine-weather-visualization/geo"
	"github.com/richard-mauri/marine-weather-visualization/sst"
)

type detectedFeature struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Geometry   struct {
		Type        string      `json:"type"`
		Coordinates [][]float64 `json:"coordinates"`
	} `json:"geometry"`
}
type detectedResponse struct {
	Type              string             `json:"type"`
	Features          []detectedFeature  `json:"features"`
	AnalysisDate      string             `json:"analysis_date"`
	Timestamp         string             `json:"sst_timestamp"`
	Bounds            geo.Bounds         `json:"bounds"`
	CandidateWindows  int                `json:"candidate_windows"`
	BoundarySegments  int                `json:"boundary_segments"`
	Sensitivity       string             `json:"sensitivity,omitempty"`
	GradientSamples   [][]float64        `json:"gradient_samples,omitempty"`
	TilesTotal        int                `json:"tiles_total"`
	TilesCompleted    int                `json:"tiles_completed"`
	TilesFailed       int                `json:"tiles_failed"`
	TileFailures      []frontTileFailure `json:"tile_failures,omitempty"`
	DuplicatesRemoved int                `json:"duplicates_removed"`
	Source            string             `json:"source"`
	Warning           string             `json:"warning"`
}
type frontTileFailure struct {
	X      int        `json:"x"`
	Y      int        `json:"y"`
	Bounds geo.Bounds `json:"bounds"`
	Reason string     `json:"reason"`
}

func classifyFrontTileFailure(err error) string {
	if err == nil {
		return "unknown"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "not cached"):
		return "not_cached"
	case strings.Contains(msg, "cooldown"), strings.Contains(msg, "temporarily unavailable"):
		return "noaa_unavailable"
	case strings.Contains(msg, "context canceled"), strings.Contains(msg, "deadline exceeded"):
		return "request_interrupted"
	default:
		return "retrieval_failed"
	}
}

type frontRequestCache struct {
	jobSlots chan struct{}
	mu       sync.Mutex
	results  map[string]detectedResponse
}

func newFrontRequestCache() *frontRequestCache {
	return &frontRequestCache{results: map[string]detectedResponse{}, jobSlots: make(chan struct{}, 1)}
}

func reconstructFrontGrid(grid sst.Grid, b geo.Bounds) ([][]float64, error) {
	const step = .01
	rows := int(math.Round((b.North-b.South)/step)) + 1
	cols := int(math.Round((b.East-b.West)/step)) + 1
	if rows < 32 || cols < 32 || rows*cols > 12000 {
		return nil, fmt.Errorf("unsupported grid dimensions %d x %d", rows, cols)
	}
	matrix := make([][]float64, rows)
	for r := range matrix {
		matrix[r] = make([]float64, cols)
		for c := range matrix[r] {
			matrix[r][c] = math.NaN()
		}
	}
	valid := 0
	for _, p := range grid.Points {
		r := int(math.Round((b.North - p.Lat) / step))
		c := int(math.Round((p.Lon - b.West) / step))
		if r >= 0 && r < rows && c >= 0 && c < cols && !math.IsNaN(p.Temp) {
			if math.IsNaN(matrix[r][c]) {
				valid++
			}
			matrix[r][c] = p.Temp
		}
	}
	if valid < 256 {
		return nil, fmt.Errorf("insufficient numerical SST coverage (%d grid points)", valid)
	}
	return matrix, nil
}
func (cache *frontRequestCache) serve(store *gridStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET required", 405)
			return
		}
		q := r.URL.Query()
		day := q.Get("date")
		if day == "" || !validAnalysisDate(day) {
			http.Error(w, "valid displayed SST date required", 400)
			return
		}
		bounds := []float64{}
		for _, k := range []string{"west", "south", "east", "north"} {
			v, e := strconv.ParseFloat(q.Get(k), 64)
			if e != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				http.Error(w, "invalid geographic bounds", 400)
				return
			}
			bounds = append(bounds, v)
		}
		sensitivity := q.Get("sensitivity")
		if sensitivity == "" {
			sensitivity = "standard"
		}
		if sensitivity != "standard" && sensitivity != "exploratory" {
			http.Error(w, "invalid sensitivity", 400)
			return
		}
		gradients := q.Get("gradient") == "1"
		b := geo.Bounds{West: bounds[0], South: bounds[1], East: bounds[2], North: bounds[3]}
		if e := b.Validate(); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		if b.East-b.West > 3.001 || b.North-b.South > 3.001 || b.East-b.West < .35 || b.North-b.South < .35 || b.South < -80 || b.North > 80 {
			http.Error(w, "zoom to an ocean region between 0.35° and 1° in width and height, then retry", 400)
			return
		}
		// Stable 0.1-degree buckets for the selected area; retrieval adds a 0.1-degree context buffer.
		b.West = math.Round(b.West*10) / 10
		b.East = math.Round(b.East*10) / 10
		b.South = math.Round(b.South*10) / 10
		b.North = math.Round(b.North*10) / 10
		if b.East-b.West < .32 || b.North-b.South < .32 || b.East-b.West > 3.01 || b.North-b.South > 3.01 {
			http.Error(w, "zoom slightly and retry", 400)
			return
		}
		key := fmt.Sprintf("N28-v1:%s:%s:%t:%.1f:%.1f:%.1f:%.1f", day, sensitivity, gradients, b.West, b.South, b.East, b.North)
		cache.mu.Lock()
		saved, ok := cache.results[key]
		cache.mu.Unlock()
		if ok {
			w.Header().Set("X-Front-Cache", "HIT")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(saved)
			return
		}

		if !ok {
			if disk, found := readFrontDisk(key); found {
				cache.mu.Lock()
				cache.results[key] = disk
				cache.mu.Unlock()
				w.Header().Set("X-Front-Cache", "DISK")
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(disk)
				return
			}
		}
		select {
		case cache.jobSlots <- struct{}{}:
			defer func() { <-cache.jobSlots }()
		case <-r.Context().Done():
			return
		}
		// Each subregion is <=0.6 degrees, with a 0.1 degree context halo.
		// Grid retrieval is sequential; tile cache is persistent and shared across jobs.
		out := detectedResponse{Type: "FeatureCollection", Sensitivity: sensitivity, Features: []detectedFeature{}, AnalysisDate: day, Bounds: b, Source: "JPL MUR SST; tiled experimental Cayula–Cornillon-inspired detector", Warning: "Research-only single-day candidates. Tile seams are not yet scientifically stitched or independently validated."}
		tileWidth := .6
		nx := int(math.Ceil((b.East - b.West) / tileWidth))
		ny := int(math.Ceil((b.North - b.South) / tileWidth))
		out.TilesTotal = nx * ny
		if out.TilesTotal > 36 {
			http.Error(w, "too many SST analysis tiles", 400)
			return
		}
		for iy := 0; iy < ny; iy++ {
			for ix := 0; ix < nx; ix++ {
				if r.Context().Err() != nil {
					return
				}
				inner := geo.Bounds{West: b.West + float64(ix)*tileWidth, East: math.Min(b.East, b.West+float64(ix+1)*tileWidth), South: b.South + float64(iy)*tileWidth, North: math.Min(b.North, b.South+float64(iy+1)*tileWidth)}
				// use a full-size context envelope even for final fractional tiles
				a := geo.Bounds{West: inner.West - .1, East: inner.East + .1, South: inner.South - .1, North: inner.North + .1}
				if a.West < -179.8 || a.East > 179.8 || a.South < -80 || a.North > 80 {
					out.TilesFailed++
					out.TileFailures = append(out.TileFailures, frontTileFailure{X: ix, Y: iy, Bounds: inner, Reason: "unsupported_bounds"})
					continue
				}
				grid, e := getCachedDetectionGrid(clientSSTContext(r), store, day, a)
				if e != nil {
					out.TilesFailed++
					out.TileFailures = append(out.TileFailures, frontTileFailure{X: ix, Y: iy, Bounds: inner, Reason: classifyFrontTileFailure(e)})
					continue
				}
				matrix, e := reconstructFrontGrid(grid, a)
				if e != nil {
					out.TilesFailed++
					out.TileFailures = append(out.TileFailures, frontTileFailure{X: ix, Y: iy, Bounds: inner, Reason: "insufficient_numerical_data"})
					continue
				}
				windowOpts := sst.DefaultWindowFrontOptions()
				boundaryOpts := sst.DefaultBoundaryOptions()
				minLength, minRank := 2.0, 5.0
				if sensitivity == "exploratory" {
					windowOpts.MinContrastC = .45
					windowOpts.MinSeparationScore = .55
					windowOpts.MinCoherence = .68
					boundaryOpts.MinContrast10NMF = .9
					boundaryOpts.MinConcentration = 1.15
					minLength, minRank = 1, 2.5
				}
				candidates := sst.FindFrontWindows(matrix, windowOpts)
				segments, _ := sst.ExtractCandidateBoundaries(matrix, candidates, a.North, .01, boundaryOpts)
				paths, _ := sst.ConnectFrontPaths(segments, a.North, .01, minLength)
				ranked := sst.RankFrontPaths(paths, minRank)
				if gradients {
					out.GradientSamples = append(out.GradientSamples, numericalGradientSamples(matrix, a, inner)...)
				}
				out.CandidateWindows += len(candidates)
				out.BoundarySegments += len(segments)
				out.TilesCompleted++
				if out.Timestamp == "" {
					out.Timestamp = grid.Timestamp
				}
				for _, p := range ranked {
					f := detectedFeature{Type: "Feature", Properties: map[string]any{"date": day, "detector_contrast_f_per_10nm": p.MeanContrast10NMF, "research_only": true}}
					f.Geometry.Type = "LineString"
					for _, pt := range p.Points {
						f.Geometry.Coordinates = append(f.Geometry.Coordinates, []float64{a.West + pt.X*.01, a.North - pt.Y*.01})
					}
					for pi, part := range clipFrontPolyline(f.Geometry.Coordinates, inner) {
						if len(part) < 2 {
							continue
						}
						child := f
						child.Geometry.Coordinates = part
						child.Properties = map[string]any{"front_id": fmt.Sprintf("tile-%d-%d-%s-%d", ix, iy, p.ID, pi+1), "date": day, "length_nm": polylineNM(part), "detector_contrast_f_per_10nm": p.MeanContrast10NMF, "research_only": true, "tile_candidate": true}
						if child.Properties["length_nm"].(float64) >= 1 {
							out.Features = append(out.Features, child)
						}
					}
				}
			}
		}
		if out.TilesCompleted == 0 {
			http.Error(w, "No numerical SST tiles were available", 502)
			return
		}
		// Exact or near-identical geometry deduplication; do not claim edge stitching.
		beforeDedupe := len(out.Features)
		out.Features = dedupeTiledFeatures(out.Features)
		out.DuplicatesRemoved = beforeDedupe - len(out.Features)
		if out.TilesFailed > 0 {
			out.Warning += fmt.Sprintf(" %d of %d SST tiles unavailable; coverage incomplete.", out.TilesFailed, out.TilesTotal)
		}
		if out.TilesFailed == 0 {
			writeFrontDisk(key, out)
		}
		cache.mu.Lock()
		if len(cache.results) > 100 {
			cache.results = map[string]detectedResponse{}
		}
		if out.TilesFailed == 0 {
			cache.results[key] = out
		}
		cache.mu.Unlock()
		w.Header().Set("X-Front-Cache", "MISS")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "private,max-age=300")
		_ = json.NewEncoder(w).Encode(out)
	}
}

// Liang–Barsky clipping of a geographic segment to a lon/lat rectangle.
func clipFrontSegment(a, c []float64, b geo.Bounds) ([][]float64, bool) {
	if len(a) < 2 || len(c) < 2 {
		return nil, false
	}
	dx, dy := c[0]-a[0], c[1]-a[1]
	t0, t1 := 0.0, 1.0
	for _, constraint := range [][2]float64{{-dx, a[0] - b.West}, {dx, b.East - a[0]}, {-dy, a[1] - b.South}, {dy, b.North - a[1]}} {
		p, q := constraint[0], constraint[1]
		if p == 0 {
			if q < 0 {
				return nil, false
			}
			continue
		}
		t := q / p
		if p < 0 {
			if t > t1 {
				return nil, false
			}
			if t > t0 {
				t0 = t
			}
		} else {
			if t < t0 {
				return nil, false
			}
			if t < t1 {
				t1 = t
			}
		}
	}
	if t0 > t1 {
		return nil, false
	}
	return [][]float64{{a[0] + t0*dx, a[1] + t0*dy}, {a[0] + t1*dx, a[1] + t1*dy}}, true
}
func clipFrontPolyline(points [][]float64, b geo.Bounds) [][][]float64 {
	var out [][][]float64
	var current [][]float64
	flush := func() {
		if len(current) > 1 {
			out = append(out, current)
		}
		current = nil
	}
	for i := 1; i < len(points); i++ {
		seg, ok := clipFrontSegment(points[i-1], points[i], b)
		if !ok {
			flush()
			continue
		}
		if len(current) > 0 && math.Hypot(current[len(current)-1][0]-seg[0][0], current[len(current)-1][1]-seg[0][1]) < 1e-8 {
			current = append(current, seg[1])
		} else {
			flush()
			current = append(current, seg[0], seg[1])
		}
	}
	flush()
	return out
}

func polylineNM(coords [][]float64) float64 {
	total := 0.0
	for i := 1; i < len(coords); i++ {
		a, c := coords[i-1], coords[i]
		meanLat := (a[1] + c[1]) * math.Pi / 360
		total += 60 * math.Hypot((c[0]-a[0])*math.Cos(meanLat), c[1]-a[1])
	}
	return total
}

// Remove identical/reversed tracks only. Two arcs with shared endpoints are
// NOT duplicates if the intermediate shape differs. Coordinates are rounded
// to roughly 0.001 degrees to tolerate insignificant numerical differences.
func featureTrackKey(coords [][]float64) string {
	var forward, reverse strings.Builder
	for i, p := range coords {
		if len(p) < 2 {
			return ""
		}
		if i > 0 {
			forward.WriteByte(';')
		}
		fmt.Fprintf(&forward, "%.3f,%.3f", p[0], p[1])
	}
	for i := len(coords) - 1; i >= 0; i-- {
		if i < len(coords)-1 {
			reverse.WriteByte(';')
		}
		fmt.Fprintf(&reverse, "%.3f,%.3f", coords[i][0], coords[i][1])
	}
	if forward.String() < reverse.String() {
		return forward.String()
	}
	return reverse.String()
}
func dedupeTiledFeatures(features []detectedFeature) []detectedFeature {
	seen := make(map[string]bool)
	out := make([]detectedFeature, 0, len(features))
	for _, f := range features {
		if len(f.Geometry.Coordinates) < 2 {
			continue
		}
		key := featureTrackKey(f.Geometry.Coordinates)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
	}
	return out
}
func frontCacheDir() string {
	if v := os.Getenv("FRONT_CACHE_DIR"); v != "" {
		return v
	}
	if home, e := os.UserHomeDir(); e == nil {
		return filepath.Join(home, ".cache", "marine-front-prototype")
	}
	return ""
}
func cacheFile(sub, key string) string {
	s := sha256.Sum256([]byte(key))
	return filepath.Join(frontCacheDir(), sub, hex.EncodeToString(s[:])+".json")
}
func readFrontDisk(key string) (detectedResponse, bool) {
	path := cacheFile("fronts", key)
	info, e := os.Stat(path)
	if e != nil || time.Since(info.ModTime()) > 14*24*time.Hour {
		return detectedResponse{}, false
	}
	data, e := os.ReadFile(path)
	if e != nil {
		return detectedResponse{}, false
	}
	var v detectedResponse
	if json.Unmarshal(data, &v) != nil {
		return detectedResponse{}, false
	}
	return v, true
}
func writeAtomically(path string, v any) {
	if frontCacheDir() == "" {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0700) != nil {
		return
	}
	data, e := json.Marshal(v)
	if e != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0600) == nil {
		if os.Rename(tmp, path) != nil {
			_ = os.Remove(tmp)
		}
	}
}
func writeFrontDisk(key string, v detectedResponse) { writeAtomically(cacheFile("fronts", key), v) }
func getCachedDetectionGrid(ctx context.Context, store *gridStore, day string, b geo.Bounds) (sst.Grid, error) {
	key := fmt.Sprintf("jplMUR-v1:%s:%.3f:%.3f:%.3f:%.3f", day, b.West, b.South, b.East, b.North)
	path := cacheFile("sst", key)
	if info, e := os.Stat(path); e == nil && time.Since(info.ModTime()) < 14*24*time.Hour {
		if data, e := os.ReadFile(path); e == nil {
			var g sst.Grid
			if json.Unmarshal(data, &g) == nil && len(g.Points) > 0 {
				return g, nil
			}
		}
	}
	grid, _, e := store.getDated(ctx, b, false, day)
	if e != nil {
		return sst.Grid{}, e
	}
	writeAtomically(path, grid)
	return grid, nil
}

// numericalGradientSamples computes finite-difference gradient magnitude from JPL MUR
// SST, entirely independently of the front candidate screening. Units: °F / 10 NM.
// Deliberately sparse: exploratory sampling, not a continuous or validated raster.
func numericalGradientSamples(m [][]float64, envelope, inner geo.Bounds) [][]float64 {
	out := make([][]float64, 0, 75)
	if len(m) < 3 || len(m[0]) < 3 {
		return out
	}
	for r := 5; r < len(m)-5; r += 8 {
		lat := envelope.North - float64(r)*.01
		if lat < inner.South || lat > inner.North {
			continue
		}
		cosLat := math.Cos(lat * math.Pi / 180)
		if cosLat < .2 {
			continue
		}
		for c := 5; c < len(m[r])-5; c += 8 {
			lon := envelope.West + float64(c)*.01
			if lon < inner.West || lon > inner.East {
				continue
			}
			a, b, d, e := m[r][c-1], m[r][c+1], m[r-1][c], m[r+1][c]
			if !finiteSST(a) || !finiteSST(b) || !finiteSST(d) || !finiteSST(e) {
				continue
			}
			gx := (b - a) * 1.8 * 10 / (1.2 * cosLat)
			gy := (e - d) * 1.8 * 10 / 1.2
			mag := math.Hypot(gx, gy)
			if mag >= .35 {
				out = append(out, []float64{lon, lat, math.Round(mag*100) / 100})
			}
		}
	}
	return out
}
func finiteSST(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

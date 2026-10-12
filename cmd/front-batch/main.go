// front-batch is an experimental, bounded NOAA SST consistency check, not independent validation.
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/richard-mauri/marine-weather-visualization/geo"
	"github.com/richard-mauri/marine-weather-visualization/sst"
)

const endpoint = "https://coastwatch.pfeg.noaa.gov/erddap/griddap/jplMURSST41.csv0"

type FeatureCollection struct {
	Features []struct {
		Geometry struct {
			Coordinates [][]float64 `json:"coordinates"`
		} `json:"geometry"`
		Properties struct {
			Date         string  `json:"date"`
			Track        string  `json:"track_id"`
			Front        string  `json:"front_id"`
			Length       float64 `json:"length_nm"`
			Contrast     float64 `json:"mean_contrast_f_per_10nm"`
			Observations int     `json:"observations"`
			Confidence   float64 `json:"experimental_confidence_score"`
		} `json:"properties"`
	} `json:"features"`
}
type Result struct {
	Date, Track, Front, Status                                                      string
	Length, Stored, Measured, Error, PeakOffset, Concentration                      float64
	Valid                                                                           int
	WidthNM, CentralFraction, CompetingRatio                                        float64
	PeakFlag                                                                        string
	RequestedTransects, SuccessfulTransects, CompetingTransects, DisplacedTransects int
	QualityFlag                                                                     string
}

type TransectResult struct {
	Date, Track, Front string
	Fraction           float64
	Result             Result
	Lat, Lon           float64
}

func nearest(grid sst.Grid, lat, lon float64) (float64, bool) {
	best := math.Inf(1)
	value := 0.
	for _, p := range grid.Points {
		d := math.Hypot((p.Lat - lat), (p.Lon-lon)*math.Cos(lat*math.Pi/180))
		if d < best {
			best = d
			value = p.Temp
		}
	}
	return value, best <= 0.016
}
func sample(grid sst.Grid, lat, lon, east, north float64) ([]float64, []bool) {
	v := make([]float64, 21)
	ok := make([]bool, 21)
	for i := -10; i <= 10; i++ {
		d := float64(i)
		y := lat + d*north/60
		x := lon + d*east/(60*math.Cos(lat*math.Pi/180))
		v[i+10], ok[i+10] = nearest(grid, y, x)
	}
	return v, ok
}
func analyze(v []float64, ok []bool, stored float64) Result {
	r := Result{Stored: stored, Status: "insufficient_data", PeakOffset: math.NaN(), Measured: math.NaN(), Error: math.NaN(), Concentration: math.NaN(), WidthNM: math.NaN(), CentralFraction: math.NaN(), CompetingRatio: math.NaN()}
	for _, b := range ok {
		if b {
			r.Valid++
		}
	}
	if r.Valid < 19 || !ok[5] || !ok[15] {
		return r
	}
	r.Measured = math.Abs(v[15]-v[5]) * 1.8
	r.Error = r.Measured - stored
	peak := 0.
	sum := 0.
	idx := 0
	var gradients [20]float64
	var available [20]bool
	for i := 0; i < 20; i++ {
		if !ok[i] || !ok[i+1] {
			continue
		}
		d := math.Abs(v[i+1] - v[i])
		gradients[i] = d
		available[i] = true
		sum += d
		if d > peak {
			peak = d
			idx = i
		}
	}
	if sum <= 0 {
		r.Status = "flat"
		return r
	}
	r.PeakOffset = float64(idx) - 9.5
	// Central fraction: share of absolute SST variation inside -2.5..+2.5 NM.
	// The five intervals centered at -2,-1,0,1,2 NM are indices 8..12.
	central := 0.0
	for i := 8; i <= 12; i++ {
		central += gradients[i]
	}
	r.CentralFraction = central / sum
	// Preserve legacy index for comparison but define it as peak-to-mean gradient.
	r.Concentration = peak * 5 / sum
	// Contiguous full width at half maximum (FWHM-like), limited by missing
	// samples and the 20 NM transect boundaries. A 1-NM gradient interval is
	// the finest possible measurement at this sampling spacing.
	left, right := idx, idx
	for left > 0 && available[left-1] && gradients[left-1] >= peak/2 {
		left--
	}
	for right < 19 && available[right+1] && gradients[right+1] >= peak/2 {
		right++
	}
	r.WidthNM = float64(right - left + 1)
	// The largest secondary slope outside +/- 3 NM is a competing-gradient
	// diagnostic; it does not assert the existence of a second ocean front.
	secondary := 0.0
	for i, d := range gradients {
		if available[i] && (i < 7 || i > 12) && d > secondary {
			secondary = d
		}
	}
	r.CompetingRatio = secondary / peak
	r.PeakFlag = "near_center"
	if math.Abs(r.PeakOffset) > 3 {
		r.PeakFlag = "displaced_peak"
	}
	if r.CompetingRatio >= 0.8 && r.PeakFlag == "near_center" {
		r.PeakFlag = "competing_offcenter_gradient"
	}
	r.Status = "ok"
	return r
}

// transectAt chooses a segment at a fraction of accumulated geometric path length.
// The direction convention matches the interactive inspector: east=-northTangent,
// north=eastTangent. It does not assume equal-length GeoJSON segments.
type Transect struct{ Lat, Lon, East, North float64 }

func transectAt(coords [][]float64, fraction float64) (Transect, bool) {
	if len(coords) < 2 || fraction < 0 || fraction > 1 {
		return Transect{}, false
	}
	lengths := make([]float64, len(coords)-1)
	total := 0.0
	for i := 1; i < len(coords); i++ {
		if len(coords[i-1]) < 2 || len(coords[i]) < 2 {
			return Transect{}, false
		}
		lat := (coords[i-1][1] + coords[i][1]) / 2
		dy := (coords[i][1] - coords[i-1][1]) * 60
		dx := (coords[i][0] - coords[i-1][0]) * 60 * math.Cos(lat*math.Pi/180)
		lengths[i-1] = math.Hypot(dx, dy)
		total += lengths[i-1]
	}
	if total < 1e-9 {
		return Transect{}, false
	}
	target := fraction * total
	cursor := 0.0
	for i, l := range lengths {
		if l < 1e-9 {
			continue
		}
		if target <= cursor+l || i == len(lengths)-1 {
			f := math.Max(0, math.Min(1, (target-cursor)/l))
			a, b := coords[i], coords[i+1]
			lat := a[1] + f*(b[1]-a[1])
			lon := a[0] + f*(b[0]-a[0])
			dy := (b[1] - a[1]) * 60
			dx := (b[0] - a[0]) * 60 * math.Cos(lat*math.Pi/180)
			n := math.Hypot(dx, dy)
			if n > 1e-9 {
				return Transect{lat, lon, -dy / n, dx / n}, true
			}
		}
		cursor += l
	}
	return Transect{}, false
}
func analyzeFeature(coords [][]float64, grid sst.Grid, stored float64) Result {
	t, ok := transectAt(coords, 0.5)
	if !ok {
		return Result{Status: "invalid_geometry"}
	}
	v, valid := sample(grid, t.Lat, t.Lon, t.East, t.North)
	return analyze(v, valid, stored)
}

// tileBounds requests a one-degree tile centered on a half-degree bin.
// The 0.25-degree halo about the containing bin covers the entire +/-10 NM
// transect at Baja latitudes, while staying well below ParseGrid's 50k limit.
// FetchGridAt uses native 0.01-degree numerical cells (stride 1) for this area.
func tileBounds(lat, lon float64) geo.Bounds {
	west := math.Floor(lon*2)/2 - 0.25
	south := math.Floor(lat*2)/2 - 0.25
	return geo.Bounds{West: west, South: south, East: west + 1, North: south + 1}
}
func main() {
	input := flag.String("geojson", "", "Milestone H front-tracks.geojson")
	out := flag.String("out", "./front-batch-validation", "output directory")
	minDays := flag.Int("min-days", 1, "minimum persistence")
	minLength := flag.Float64("min-length", 10, "minimum front length NM")
	flag.Parse()
	if *input == "" {
		fmt.Fprintln(os.Stderr, "-geojson is required")
		os.Exit(2)
	}
	raw, err := os.ReadFile(*input)
	must(err)
	var fc FeatureCollection
	must(json.Unmarshal(raw, &fc))
	must(os.MkdirAll(*out, 0755))
	client := &http.Client{Timeout: 80 * time.Second}
	cache := map[string]sst.Grid{}
	// Remember unsuccessful tile requests too; a shared failed tile is fetched once.
	failedTile := map[string]string{}
	affectedProfiles := 0
	results := []Result{}
	failures := map[string]string{}
	transects := []TransectResult{}
	fractions := []float64{0.25, 0.5, 0.75}
	for _, f := range fc.Features {
		p := f.Properties
		c := f.Geometry.Coordinates
		if p.Observations < *minDays || p.Length < *minLength || len(c) < 2 {
			continue
		}
		measured := []float64{}
		offsets := []float64{}
		widths := []float64{}
		centrals := []float64{}
		sampleOK := 0
		competingCount := 0
		displacedCount := 0
		failuresForFront := 0
		for _, fraction := range fractions {
			t, geometryOK := transectAt(c, fraction)
			if !geometryOK {
				transects = append(transects, TransectResult{p.Date, p.Track, p.Front, fraction, Result{Status: "invalid_geometry"}, 0, 0})
				continue
			}
			b := tileBounds(t.Lat, t.Lon)
			key := fmt.Sprintf("%s|%.2f|%.2f", p.Date, b.West, b.South)
			g, cached := cache[key]
			if _, failed := failedTile[key]; failed {
				failuresForFront++
				transects = append(transects, TransectResult{p.Date, p.Track, p.Front, fraction, Result{Status: "upstream_unavailable"}, t.Lat, t.Lon})
				continue
			}
			if !cached {
				fmt.Printf("NOAA %s region %.2f,%.2f to %.2f,%.2f\n", p.Date, b.West, b.South, b.East, b.North)
				grid, e := sst.FetchGridAt(context.Background(), client, endpoint, b, 260, p.Date)
				if e != nil {
					failures[key] = e.Error()
					failedTile[key] = e.Error()
					fmt.Fprintln(os.Stderr, "upstream unavailable:", e)
					failuresForFront++
					transects = append(transects, TransectResult{p.Date, p.Track, p.Front, fraction, Result{Status: "upstream_unavailable"}, t.Lat, t.Lon})
					continue
				}
				g = grid
				cache[key] = g
				time.Sleep(250 * time.Millisecond)
			}
			v, ok := sample(g, t.Lat, t.Lon, t.East, t.North)
			r := analyze(v, ok, p.Contrast)
			transects = append(transects, TransectResult{p.Date, p.Track, p.Front, fraction, r, t.Lat, t.Lon})
			if r.Status == "ok" {
				if r.PeakFlag == "competing_offcenter_gradient" {
					competingCount++
				}
				if r.PeakFlag == "displaced_peak" {
					displacedCount++
				}
				sampleOK++
				measured = append(measured, r.Measured)
				offsets = append(offsets, r.PeakOffset)
				widths = append(widths, r.WidthNM)
				centrals = append(centrals, r.CentralFraction)
			}
		}
		if failuresForFront > 0 {
			affectedProfiles++
		}
		r := Result{Date: p.Date, Track: p.Track, Front: p.Front, Length: p.Length, Stored: p.Contrast, Status: "insufficient_data", Measured: math.NaN(), Error: math.NaN(), PeakOffset: math.NaN(), Concentration: math.NaN(), WidthNM: math.NaN(), CentralFraction: math.NaN(), CompetingRatio: math.NaN()}
		r.RequestedTransects = len(fractions)
		r.SuccessfulTransects = sampleOK
		r.CompetingTransects = competingCount
		r.DisplacedTransects = displacedCount
		r.QualityFlag = qualityFlag(sampleOK, len(fractions), competingCount, displacedCount)
		if sampleOK > 0 {
			sort.Float64s(measured)
			sort.Float64s(offsets)
			sort.Float64s(widths)
			sort.Float64s(centrals)
			r.WidthNM = widths[len(widths)/2]
			r.CentralFraction = centrals[len(centrals)/2]
			r.Measured = measured[len(measured)/2]
			r.Error = r.Measured - p.Contrast
			r.PeakOffset = offsets[len(offsets)/2]
			r.Valid = sampleOK
			r.Status = "ok"
		} else if failuresForFront > 0 {
			r.Status = "upstream_unavailable"
		}
		results = append(results, r)
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Date == results[j].Date {
			return results[i].Track < results[j].Track
		}
		return results[i].Date < results[j].Date
	})
	file, e := os.Create(filepath.Join(*out, "validation.csv"))
	must(e)
	w := csv.NewWriter(file)
	validationHeader := []string{"date", "track_id", "front_id", "length_nm", "stored_f_per_10nm", "measured_f_per_10nm", "difference_f", "peak_offset_nm", "peak_concentration_index", "valid_samples", "status", "median_transition_width_nm", "median_central_gradient_fraction", "transects_requested", "transects_valid", "competing_gradient_transects", "displaced_peak_transects", "quality_flag"}
	must(w.Write(validationHeader))
	for _, r := range results {
		row := []string{r.Date, r.Track, r.Front, format(r.Length), format(r.Stored), format(r.Measured), format(r.Error), format(r.PeakOffset), format(r.Concentration), strconv.Itoa(r.Valid), r.Status, format(r.WidthNM), format(r.CentralFraction), strconv.Itoa(r.RequestedTransects), strconv.Itoa(r.SuccessfulTransects), strconv.Itoa(r.CompetingTransects), strconv.Itoa(r.DisplacedTransects), r.QualityFlag}
		must(w.Write(row))
	}
	w.Flush()
	must(w.Error())
	must(file.Close())
	tf, e := os.Create(filepath.Join(*out, "transects.csv"))
	must(e)
	tw := csv.NewWriter(tf)
	transectHeader := []string{"date", "track_id", "front_id", "fraction_along_front", "center_lat", "center_lon", "measured_f_per_10nm", "difference_f", "peak_offset_nm", "valid_samples", "status", "transition_width_nm", "central_gradient_fraction", "competing_gradient_ratio", "peak_flag"}
	must(tw.Write(transectHeader))
	for _, r := range transects {
		must(tw.Write([]string{r.Date, r.Track, r.Front, format(r.Fraction), format(r.Lat), format(r.Lon), format(r.Result.Measured), format(r.Result.Error), format(r.Result.PeakOffset), strconv.Itoa(r.Result.Valid), r.Result.Status, format(r.Result.WidthNM), format(r.Result.CentralFraction), format(r.Result.CompetingRatio), r.Result.PeakFlag}))
	}
	tw.Flush()
	must(tw.Error())
	must(tf.Close())
	n := 0
	mae := 0.
	offset := 0.
	for _, r := range results {
		if r.Status == "ok" {
			n++
			mae += math.Abs(r.Error)
			offset += math.Abs(r.PeakOffset)
		}
	}
	summary := map[string]any{"generated_utc": time.Now().UTC().Format(time.RFC3339), "input_features": len(fc.Features), "evaluated": len(results), "valid_profiles": n, "upstream_failures": failures, "profiles_affected_by_upstream_failures": affectedProfiles, "tile_count_successful": len(cache), "tile_count_failed": len(failedTile), "transects_requested": len(transects), "transects_valid": countValidTransects(transects), "diagnostics": diagnosticCounts(transects), "quality_screen": qualityCounts(results), "sampling_fractions": fractions, "note": "Internal consistency only; transect diagnostic width is contiguous 1-NM gradient intervals >= half the maximum, central-gradient fraction is absolute gradient in central 5NM divided by absolute gradient in entire 20NM; neither is independent front truth. Median aggregates of valid positions only. JPL MUR is not independent ground truth."}
	if n > 0 {
		summary["mean_absolute_difference_f"] = mae / float64(n)
		summary["mean_abs_peak_offset_nm"] = offset / float64(n)
	}
	j, e := json.MarshalIndent(summary, "", "  ")
	must(e)
	must(os.WriteFile(filepath.Join(*out, "summary.json"), j, 0644))
	fmt.Printf("Evaluated %d profiles; %d valid, %d affected by %d failed upstream tiles. Results: %s\n", len(results), n, affectedProfiles, len(failures), *out)
}

// qualityFlag is a transparent screening label, not a calibrated probability or
// oceanographic strength class. Failures and off-center gradients are kept distinct.
func qualityFlag(valid, requested, competing, displaced int) string {
	if valid == 0 {
		return "no_usable_transects"
	}
	if displaced > 0 {
		return "review_displaced_peak"
	}
	if competing > 0 {
		return "review_competing_gradient"
	}
	if valid < requested {
		return "partial_sampling"
	}
	return "no_diagnostic_flags"
}
func qualityCounts(rows []Result) map[string]int {
	counts := map[string]int{"no_usable_transects": 0, "review_displaced_peak": 0, "review_competing_gradient": 0, "partial_sampling": 0, "no_diagnostic_flags": 0}
	for _, r := range rows {
		counts[r.QualityFlag]++
	}
	return counts
}
func diagnosticCounts(rows []TransectResult) map[string]int {
	counts := map[string]int{"near_center": 0, "displaced_peak": 0, "competing_offcenter_gradient": 0}
	for _, t := range rows {
		if t.Result.Status == "ok" {
			counts[t.Result.PeakFlag]++
		}
	}
	return counts
}
func countValidTransects(rows []TransectResult) int {
	n := 0
	for _, r := range rows {
		if r.Result.Status == "ok" {
			n++
		}
	}
	return n
}
func format(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return ""
	}
	return strconv.FormatFloat(f, 'f', 3, 64)
}
func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

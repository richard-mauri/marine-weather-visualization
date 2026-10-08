package sst

import (
	"context"
	"encoding/csv"
	"fmt"
	"image"
	"image/color"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/richard-mauri/marine-weather-visualization/geo"
)

type Point struct{ Lat, Lon, Temp float64 }
type Grid struct {
	Points    []Point
	Timestamp string
	Min, Max  float64
	Step      float64
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

// MUR's griddap coordinate axes do not include exact +/-180 degrees.
// Keep display-tile extents separate from the clipped upstream subset.
// These interior limits avoid invalid coordinate-value requests; they are
// deliberately conservative until live axis metadata is implemented.
const (
	murMinLon = -179.99
	murMaxLon = 179.99
	murMinLat = -89.99
	murMaxLat = 89.99
)

func BuildGridURL(endpoint string, b geo.Bounds, maxCells int) (string, error) {
	if err := b.Validate(); err != nil {
		return "", err
	}
	if maxCells <= 0 {
		return "", fmt.Errorf("maxCells must be positive")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || !strings.HasSuffix(u.Path, ".csv0") {
		return "", fmt.Errorf("invalid griddap CSV0 endpoint")
	}
	// Calculate stride from the original display region so neighboring
	// same-level tiles retain the same grid spacing.
	stride := int(math.Max(1, math.Ceil(math.Max((b.North-b.South)*100, (b.East-b.West)*100)/float64(maxCells))))
	// Add one sample-stride of overlap to avoid a missing half-cell at tile
	// boundaries. RenderGrid() still uses the unchanged display bounds.
	halo := float64(stride) * 0.01
	south := math.Max(murMinLat, b.South-halo)
	north := math.Min(murMaxLat, b.North+halo)
	west := math.Max(murMinLon, b.West-halo)
	east := math.Min(murMaxLon, b.East+halo)
	if south >= north || west >= east {
		return "", fmt.Errorf("tile outside MUR SST grid domain")
	}
	expr := fmt.Sprintf("analysed_sst[(last)][(%.3f):%d:(%.3f)][(%.3f):%d:(%.3f)]", south, stride, north, west, stride, east)
	u.RawQuery = expr
	return u.String(), nil
}
func ParseGrid(r io.Reader) (Grid, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	g := Grid{Min: math.Inf(1), Max: math.Inf(-1), Step: 0.01}
	for {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Grid{}, err
		}
		if len(row) < 4 {
			continue
		}
		// csv0 ERDDAP grid rows: time,latitude,longitude,analysed_sst. Header rows are skipped.
		lat, e1 := strconv.ParseFloat(strings.TrimSpace(row[1]), 64)
		lon, e2 := strconv.ParseFloat(strings.TrimSpace(row[2]), 64)
		t, e3 := strconv.ParseFloat(strings.TrimSpace(row[3]), 64)
		if e1 != nil || e2 != nil || e3 != nil || math.IsNaN(t) || math.IsInf(t, 0) || t < -3 || t > 45 {
			continue
		}
		if g.Timestamp == "" {
			g.Timestamp = strings.TrimSpace(row[0])
		}
		g.Points = append(g.Points, Point{lat, lon, t})
		g.Min = math.Min(g.Min, t)
		g.Max = math.Max(g.Max, t)
		if len(g.Points) > 50000 {
			return Grid{}, fmt.Errorf("numerical grid too large")
		}
	}
	if len(g.Points) == 0 {
		return Grid{}, fmt.Errorf("no valid numerical SST samples in requested region")
	}
	return g, nil
}
func FetchGrid(ctx context.Context, client *http.Client, endpoint string, b geo.Bounds, maxCells int) (Grid, error) {
	u, err := BuildGridURL(endpoint, b, maxCells)
	if err != nil {
		return Grid{}, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return Grid{}, err
	}
	req.Header.Set("User-Agent", "MarineRasterPrototype/0.3.6")
	resp, err := client.Do(req)
	if err != nil {
		return Grid{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		safe := regexp.MustCompile(`[\x00-\x1f]+`).ReplaceAllString(string(snippet), " ")
		if len(safe) > 240 {
			safe = safe[:240]
		}
		log.Printf("ERDDAP non-200 status=%d bounds=%.3f,%.3f,%.3f,%.3f path=%s response=%q", resp.StatusCode, b.West, b.South, b.East, b.North, req.URL.Path, safe)
		return Grid{}, fmt.Errorf("ERDDAP returned HTTP %d for geographic bounds %.2f,%.2f to %.2f,%.2f (see server log)", resp.StatusCode, b.West, b.South, b.East, b.North)
	}
	return ParseGrid(io.LimitReader(resp.Body, 8<<20))
}
func merc(lat float64) float64 {
	rad := clamp(lat, -85, 85) * math.Pi / 180
	return math.Log(math.Tan(math.Pi/4 + rad/2))
}
func tempColor(t float64) color.NRGBA { // fixed, documented physical Celsius scale 5..30
	stops := [][3]uint8{{48, 48, 133}, {42, 122, 190}, {43, 183, 155}, {174, 216, 93}, {246, 208, 62}, {236, 113, 44}, {171, 31, 57}}
	x := clamp((t-5)/25, 0, 1) * float64(len(stops)-1)
	i := int(math.Min(float64(len(stops)-2), math.Floor(x)))
	f := x - float64(i)
	mix := func(a, b uint8) uint8 { return uint8(math.Round(float64(a)*(1-f) + float64(b)*f)) }
	return color.NRGBA{mix(stops[i][0], stops[i+1][0]), mix(stops[i][1], stops[i+1][1]), mix(stops[i][2], stops[i+1][2]), 220}
}
func RenderGrid(g Grid, b geo.Bounds, width, height int) *image.NRGBA {
	im := image.NewNRGBA(image.Rect(0, 0, width, height))
	if len(g.Points) == 0 {
		return im
	}
	// Each numeric cell paints only its own footprint: no fabricated interpolation across missing values.
	latLo, latHi := merc(b.South), merc(b.North)
	project := func(lat, lon float64) (int, int) {
		return int(math.Round((lon - b.West) / (b.East - b.West) * float64(width))), int(math.Round((latHi - merc(lat)) / (latHi - latLo) * float64(height)))
	}
	lats, lons := map[float64]bool{}, map[float64]bool{}
	for _, p := range g.Points {
		lats[p.Lat] = true
		lons[p.Lon] = true
	}
	getSpacing := func(m map[float64]bool) float64 {
		vs := make([]float64, 0, len(m))
		for v := range m {
			vs = append(vs, v)
		}
		sort.Float64s(vs)
		best := math.Inf(1)
		for i := 1; i < len(vs); i++ {
			d := vs[i] - vs[i-1]
			if d > 1e-7 && d < best {
				best = d
			}
		}
		if math.IsInf(best, 1) {
			return 0.01
		}
		return best
	}
	stepLat := getSpacing(lats)
	stepLon := getSpacing(lons)
	for _, p := range g.Points {
		x0, y0 := project(p.Lat+stepLat/2, p.Lon-stepLon/2)
		x1, y1 := project(p.Lat-stepLat/2, p.Lon+stepLon/2)
		if x0 < 0 {
			x0 = 0
		}
		if y0 < 0 {
			y0 = 0
		}
		if x1 > width {
			x1 = width
		}
		if y1 > height {
			y1 = height
		}
		c := tempColor(p.Temp)
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				im.SetNRGBA(x, y, c)
			}
		}
	}
	return im
}

package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"log"
	"math"
	"math/rand"
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

//go:embed index.html leaflet-local.css coastline.geojson
var content embed.FS

const version = "0.4.20"

var diagnosticGeoJSON []byte
var shorelineGeoJSON []byte

// Allowed spans must match the browser's multiresolution tile pyramid.
func validAnalysisDate(day string) bool {
	if day == "" {
		return true
	}
	d, e := time.Parse("2006-01-02", day)
	return e == nil && d.Format("2006-01-02") == day && !d.After(time.Now().UTC().Add(24*time.Hour))
}

func validTileSpan(span int) bool {
	switch span {
	case 1, 2, 4, 8, 16, 32, 64, 128:
		return true
	}
	return false
}

// Tiles at the data envelope are clipped to supported coordinates, rather
// than rejecting a 128-degree tile solely because it crosses latitude 85.
func clippedTileBounds(span, x, y int) (geo.Bounds, bool) {
	west := math.Max(-180, float64(x)*float64(span))
	east := math.Min(180, float64(x+1)*float64(span))
	south := math.Max(-85, float64(y)*float64(span))
	north := math.Min(85, float64(y+1)*float64(span))
	b := geo.Bounds{West: west, East: east, South: south, North: north}
	return b, west < east && south < north
}

const defaultGrid = "https://coastwatch.pfeg.noaa.gov/erddap/griddap/jplMURSST41.csv0"

// gridStore is per-process. Identical and nearby viewports reuse a common
// rounded geographic envelope. Expired entries can serve as fallback on 503.
type gridEntry struct {
	grid   sst.Grid
	stored time.Time
}
type gridFlight struct {
	done chan struct{}
	grid sst.Grid
	err  error
}
type gridStore struct {
	mu                                                sync.Mutex
	entries                                           map[string]gridEntry
	flights                                           map[string]*gridFlight
	slots                                             chan struct{}
	ttl                                               time.Duration
	fetch                                             func(context.Context, geo.Bounds) (sst.Grid, error)
	cooldown                                          map[string]time.Time
	hits, misses, coalesced, stale, retries, failures uint64
	active                                            int
}

func newGridStore(fetch func(context.Context, geo.Bounds) (sst.Grid, error)) *gridStore {
	return &gridStore{entries: make(map[string]gridEntry), flights: make(map[string]*gridFlight), cooldown: make(map[string]time.Time), slots: make(chan struct{}, 2), ttl: 45 * time.Minute, fetch: fetch}
}
func bucketBounds(b geo.Bounds) geo.Bounds {
	// Coarse request envelopes enable cache reuse across small pans.
	const bucket = .2
	return geo.Bounds{West: math.Floor(b.West/bucket) * bucket, South: math.Floor(b.South/bucket) * bucket, East: math.Ceil(b.East/bucket) * bucket, North: math.Ceil(b.North/bucket) * bucket}
}

type dateContextKey struct{}

func (c *gridStore) get(ctx context.Context, b geo.Bounds, force bool) (sst.Grid, string, error) {
	return c.getDated(ctx, b, force, "")
}

func (c *gridStore) getDated(ctx context.Context, b geo.Bounds, force bool, day string) (sst.Grid, string, error) {
	if day != "" {
		ctx = context.WithValue(ctx, dateContextKey{}, day)
	}
	b = bucketBounds(b)
	key := fmt.Sprintf("%s/%.2f/%.2f/%.2f/%.2f", day, b.West, b.South, b.East, b.North)
	c.mu.Lock()
	old, has := c.entries[key]
	if has && !force && time.Since(old.stored) < c.ttl {
		c.hits++
		c.mu.Unlock()
		return old.grid, "HIT", nil
	}
	if f, ok := c.flights[key]; ok {
		c.coalesced++
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return sst.Grid{}, "", ctx.Err()
		case <-f.done:
		}
		if f.err == nil {
			return f.grid, "COALESCED", nil
		}
		if has {
			c.mu.Lock()
			c.stale++
			c.mu.Unlock()
			return old.grid, "STALE", nil
		}
		return sst.Grid{}, "", f.err
	}
	if until, ok := c.cooldown[key]; ok && time.Now().Before(until) {
		if has {
			c.stale++
			c.mu.Unlock()
			return old.grid, "STALE-COOLDOWN", nil
		}
		c.mu.Unlock()
		return sst.Grid{}, "COOLDOWN", fmt.Errorf("ERDDAP temporarily unavailable; retry after %s", time.Until(until).Round(time.Second))
	}
	c.misses++
	f := &gridFlight{done: make(chan struct{})}
	c.flights[key] = f
	c.mu.Unlock()
	// Wait for a single process-wide upstream slot, honoring the requesting client.
	acquired := false
	select {
	case c.slots <- struct{}{}:
		acquired = true
	case <-ctx.Done():
		f.err = ctx.Err()
	}
	if acquired {
		c.mu.Lock()
		c.active++
		c.mu.Unlock()
		for attempt := 0; attempt < 3; attempt++ {
			if ctx.Err() != nil {
				f.err = ctx.Err()
				break
			}
			fetchCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
			f.grid, f.err = c.fetch(fetchCtx, b)
			cancel()
			if f.err == nil || !transientERDDAP(f.err) {
				break
			}
			if attempt == 2 {
				break
			}
			c.mu.Lock()
			c.retries++
			c.mu.Unlock()
			delay := time.Duration(400*(1<<attempt))*time.Millisecond + time.Duration(rand.Intn(300))*time.Millisecond
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				f.err = ctx.Err()
				if !timer.Stop() {
					<-timer.C
				}
				attempt = 3
			case <-timer.C:
			}
		}
		c.mu.Lock()
		c.active--
		c.mu.Unlock()
		<-c.slots
	}
	c.mu.Lock()
	if f.err == nil {
		delete(c.cooldown, key)
		c.entries[key] = gridEntry{f.grid, time.Now()}
	} else if transientERDDAP(f.err) {
		c.failures++
		c.cooldown[key] = time.Now().Add(20 * time.Second)
	}
	delete(c.flights, key)
	close(f.done)
	// Bound cache memory and discard old entries opportunistically.
	for k, v := range c.entries {
		if time.Since(v.stored) > 4*c.ttl {
			delete(c.entries, k)
		}
	}
	if len(c.entries) > 96 {
		var oldest string
		var at time.Time
		for k, v := range c.entries {
			if oldest == "" || v.stored.Before(at) {
				oldest, at = k, v.stored
			}
		}
		delete(c.entries, oldest)
	}
	c.mu.Unlock()
	if f.err != nil {
		if has {
			c.mu.Lock()
			c.stale++
			c.mu.Unlock()
			return old.grid, "STALE", nil
		}
		return sst.Grid{}, "", f.err
	}
	return f.grid, "MISS", nil
}

func transientERDDAP(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "HTTP 503") || strings.Contains(msg, "HTTP 502") || strings.Contains(msg, "HTTP 429")
}

func (c *gridStore) stats() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	cooldowns := 0
	for _, until := range c.cooldown {
		if now.Before(until) {
			cooldowns++
		}
	}
	return map[string]any{"cache_entries": len(c.entries), "inflight": len(c.flights), "active_upstream": c.active,
		"max_upstream": cap(c.slots), "hits": c.hits, "misses": c.misses, "coalesced": c.coalesced,
		"stale_served": c.stale, "retries": c.retries, "transient_failures": c.failures, "cooldown_keys": cooldowns}
}

func handler(endpoint string, client *http.Client) http.Handler {
	return handlerWithMask(endpoint, client, nil)
}

func handlerWithMask(endpoint string, client *http.Client, mask *sst.LandMask) http.Handler {
	store := newGridStore(func(ctx context.Context, b geo.Bounds) (sst.Grid, error) {
		day, _ := ctx.Value(dateContextKey{}).(string)
		return sst.FetchGridAt(ctx, client, endpoint, b, 180, day)
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sst-cache/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(store.stats())
	})
	mux.Handle("/", http.FileServer(http.FS(content)))
	mux.HandleFunc("/api/coastline", func(w http.ResponseWriter, r *http.Request) {
		if len(diagnosticGeoJSON) == 0 {
			http.Error(w, "No coastline geometry configured. Set SST_LAND_GEOJSON to an independently verified GeoJSON land polygon file.", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/geo+json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(diagnosticGeoJSON)
	})
	mux.HandleFunc("/api/shoreline", func(w http.ResponseWriter, r *http.Request) {
		if len(shorelineGeoJSON) == 0 {
			http.Error(w, "Dedicated shoreline linework not installed; use the polygon diagnostic", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/geo+json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(shorelineGeoJSON)
	})
	mux.HandleFunc("/api/coastline/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if mask == nil {
			_, _ = w.Write([]byte(`{"enabled":false,"message":"No verified high-resolution coastline loaded; SST source no-data mask only"}`))
			return
		}
		_, _ = w.Write([]byte(`{"enabled":true,"message":"User-supplied polygon mask active; visually verify coastline against base map","shoreline_available":` + strconv.FormatBool(len(shorelineGeoJSON) > 0) + `}`))
	})
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","version":"%s"}`, version)
	})
	mux.HandleFunc("/api/source", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"name": "JPL MUR SST fv04.1 via NOAA ERDDAP numerical grid", "units": "degrees Celsius", "resolution": "nominal 0.01 degree (~1 km)", "time": "last available dataset timestep; UTC timestamp parsed from CSV", "mask": "missing data transparent; optional external polygon clipping"})
	})
	mux.HandleFunc("/api/sst-image", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		parse := func(k string) (float64, error) { return strconv.ParseFloat(q.Get(k), 64) }
		west, e1 := parse("west")
		south, e2 := parse("south")
		east, e3 := parse("east")
		north, e4 := parse("north")
		width, e5 := strconv.Atoi(q.Get("width"))
		height, e6 := strconv.Atoi(q.Get("height"))
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || e6 != nil {
			http.Error(w, "invalid parameters", 400)
			return
		}
		b := geo.Bounds{West: west, South: south, East: east, North: north}
		if err := b.Validate(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if south < -85 || north > 85 || east-west > 8 || north-south > 8 || width < 1 || height < 1 || width > 1400 || height > 1100 || width*height > 1000000 || (east-west)*(north-south) > 12 || math.IsNaN(west) || math.IsNaN(east) || math.IsNaN(south) || math.IsNaN(north) {
			http.Error(w, "unsupported extent or dimensions: zoom closer", 400)
			return
		}
		day := q.Get("date")
		if !validAnalysisDate(day) {
			http.Error(w, "invalid SST analysis date", http.StatusBadRequest)
			return
		}
		grid, cacheState, err := store.getDated(r.Context(), b, q.Get("refresh") == "1", day)
		if err != nil {
			http.Error(w, "numerical SST unavailable: "+err.Error(), 502)
			return
		}
		img := sst.RenderGrid(grid, b, width, height)
		if mask != nil {
			mask.Apply(img, b)
			sst.FillNearshore(img, b, mask)
		}
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, img); err != nil {
			log.Printf("SST PNG encoding failed: %v", err)
			http.Error(w, "SST image encoding failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "private, max-age=300")
		w.Header().Set("X-SST-Time", grid.Timestamp)
		w.Header().Set("X-SST-Cache", cacheState)
		w.Header().Set("X-SST-Count", strconv.Itoa(len(grid.Points)))
		_, _ = w.Write(encoded.Bytes())
	})
	mux.HandleFunc("/api/sst-tile", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		day := q.Get("date")
		if !validAnalysisDate(day) {
			http.Error(w, "invalid SST analysis date", http.StatusBadRequest)
			return
		}
		span, err := strconv.Atoi(q.Get("span"))
		if err != nil || !validTileSpan(span) {
			http.Error(w, "invalid tile span", http.StatusBadRequest)
			return
		}
		x, ex := strconv.Atoi(q.Get("x"))
		y, ey := strconv.Atoi(q.Get("y"))
		if ex != nil || ey != nil || x < -360 || x > 360 || y < -180 || y > 180 {
			http.Error(w, "invalid tile coordinates", http.StatusBadRequest)
			return
		}
		// Geographic display tiles may lie in any wrapped copy of the world.
		// Split at each antimeridian before querying the canonical ERDDAP axes.
		west := float64(x * span)
		east := float64((x + 1) * span)
		south := math.Max(-85, float64(y*span))
		north := math.Min(85, float64((y+1)*span))
		if south >= north {
			w.Header().Set("X-SST-No-Coverage", "outside-supported-latitudes")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// High-resolution regional tiles prevent shoreline geometry from being
		// rasterized at the coarse 256px resolution of a 1-degree tile.
		pixels := 256
		if text := q.Get("pixels"); text != "" {
			requested, e := strconv.Atoi(text)
			if e != nil || (requested != 256 && requested != 512 && requested != 768) {
				http.Error(w, "invalid tile pixel size", http.StatusBadRequest)
				return
			}
			pixels = requested
		}
		img := image.NewNRGBA(image.Rect(0, 0, pixels, pixels))
		timestamp, cacheState := "", ""
		validSegments := 0
		for cursor := west; cursor < east-1e-9; {
			normalized := math.Mod(cursor+180, 360)
			if normalized < 0 {
				normalized += 360
			}
			normalized -= 180
			segmentEnd := math.Min(east, cursor+(180-normalized))
			dataWest, dataEast := normalized, normalized+(segmentEnd-cursor)
			dataBounds := geo.Bounds{West: dataWest, East: dataEast, South: south, North: north}
			left := int(math.Round((cursor - west) / (east - west) * float64(pixels)))
			right := int(math.Round((segmentEnd - west) / (east - west) * float64(pixels)))
			if right > left {
				grid, state, fetchErr := store.getDated(r.Context(), dataBounds, q.Get("refresh") == "1", day)
				if fetchErr != nil {
					// Browser navigation or date changes intentionally cancel obsolete tile requests.
					// Do not log these as upstream failures or return a misleading 502.
					if errors.Is(fetchErr, context.Canceled) || errors.Is(r.Context().Err(), context.Canceled) {
						return
					}
					if !strings.Contains(fetchErr.Error(), "no valid numerical SST samples") {
						log.Printf("SST wrapped tile failure span=%d x=%d y=%d dataBounds=%.4f,%.4f,%.4f,%.4f: %v", span, x, y, dataWest, south, dataEast, north, fetchErr)
						http.Error(w, "SST tile unavailable: "+fetchErr.Error(), 502)
						return
					}
				} else {
					segmentImg := sst.RenderGrid(grid, dataBounds, right-left, pixels)
					if mask != nil {
						mask.Apply(segmentImg, dataBounds)
						sst.FillNearshore(segmentImg, dataBounds, mask)

					}
					draw.Draw(img, image.Rect(left, 0, right, pixels), segmentImg, image.Point{}, draw.Src)
					validSegments++
					timestamp = grid.Timestamp
					cacheState = state
				}
			}
			cursor = segmentEnd
		}
		if validSegments == 0 {
			w.Header().Set("X-SST-No-Coverage", "no-valid-sst")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if q.Get("mask_edge") == "1" && mask != nil {
			sst.DrawAlphaEdge(img)
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			http.Error(w, "tile encoding failed", 500)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "private, max-age=300")
		w.Header().Set("X-SST-Time", timestamp)
		w.Header().Set("X-SST-Cache", cacheState)
		_, _ = w.Write(buf.Bytes())
	})
	mux.HandleFunc("/api/sst-sample", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		lat, e1 := strconv.ParseFloat(q.Get("lat"), 64)
		lon, e2 := strconv.ParseFloat(q.Get("lon"), 64)
		if e1 != nil || e2 != nil || math.IsNaN(lat) || math.IsNaN(lon) || lat < -85 || lat > 85 || lon < -180 || lon > 180 {
			http.Error(w, "invalid coordinate", 400)
			return
		}
		day := q.Get("date")
		if !validAnalysisDate(day) {
			http.Error(w, "invalid SST analysis date", 400)
			return
		}
		// Query a small area, never the entire viewport. Avoid implying that
		// nearshore display estimates are actual satellite observations.
		west := math.Max(-179.99, lon-0.015)
		east := math.Min(179.99, lon+0.015)
		south := math.Max(-85, lat-0.015)
		north := math.Min(85, lat+0.015)
		if west >= east || south >= north {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"available": false})
			return
		}
		grid, _, err := store.getDated(r.Context(), geo.Bounds{West: west, East: east, South: south, North: north}, false, day)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(r.Context().Err(), context.Canceled) {
				return
			}
			if strings.Contains(err.Error(), "no valid numerical SST samples") {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"available": false})
				return
			}
			http.Error(w, "sampling unavailable", 503)
			return
		}
		closest := math.MaxFloat64
		temperature := 0.0
		found := false
		for _, pt := range grid.Points {
			d := math.Hypot((pt.Lat - lat), (pt.Lon-lon)*math.Cos(lat*math.Pi/180))
			if d < closest {
				closest = d
				temperature = pt.Temp
				found = true
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if !found || closest > 0.016 {
			_ = json.NewEncoder(w).Encode(map[string]any{"available": false, "timestamp": grid.Timestamp})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"available": true, "celsius": temperature, "timestamp": grid.Timestamp, "type": "nearest_noaa_grid_sample"})
	})
	mux.HandleFunc("/api/sst-meta", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		parse := func(k string) (float64, error) { return strconv.ParseFloat(q.Get(k), 64) }
		west, e1 := parse("west")
		south, e2 := parse("south")
		east, e3 := parse("east")
		north, e4 := parse("north")
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			http.Error(w, "invalid bbox", 400)
			return
		}
		b := geo.Bounds{West: west, South: south, East: east, North: north}
		if err := b.Validate(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if east-west > 8 || north-south > 8 || (east-west)*(north-south) > 12 {
			http.Error(w, "zoom closer", 400)
			return
		}
		day := q.Get("date")
		if !validAnalysisDate(day) {
			http.Error(w, "invalid SST analysis date", http.StatusBadRequest)
			return
		}
		grid, cacheState, err := store.getDated(r.Context(), b, false, day)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-SST-Cache", cacheState)
		_ = json.NewEncoder(w).Encode(map[string]any{"timestamp": grid.Timestamp, "points": len(grid.Points), "min_c": grid.Min, "max_c": grid.Max})
	})
	return mux
}
func main() {
	addr := flag.String("addr", "127.0.0.1:8081", "local listen address")
	flag.Parse()
	endpoint := os.Getenv("SST_GRID_ENDPOINT")
	if endpoint == "" {
		endpoint = defaultGrid
	}
	client := &http.Client{Timeout: 45 * time.Second}
	maskPath := os.Getenv("SST_LAND_GEOJSON")
	// A project-local, user-verified dataset is automatically discovered.
	// We deliberately do NOT auto-enable the bundled coarse comparison geometry.
	if maskPath == "" {
		if _, err := os.Stat("data/land.geojson"); err == nil {
			maskPath = "data/land.geojson"
		}
	}
	// Deliberately leave the inaccurate GSHHS mask disabled by default.
	// Supply a verified high-resolution land polygon file to enable masking.
	// SST_USE_GSHHS_APPROX=1 can re-enable the coarse geometry for comparison.
	if maskPath == "" && os.Getenv("SST_USE_GSHHS_APPROX") == "1" {
		maskPath = "bundled GSHHS intermediate"
	}
	var mask *sst.LandMask
	if maskPath != "" {
		var raw []byte
		var err error
		if maskPath == "bundled GSHHS intermediate" {
			raw, err = content.ReadFile("coastline.geojson")
		} else {
			raw, err = os.ReadFile(filepath.Clean(maskPath))
		}
		if err != nil {
			log.Fatalf("land mask read: %v", err)
		}
		mask, err = sst.ParseLandGeoJSON(raw)
		if err != nil {
			log.Fatalf("land mask parse: %v", err)
		}
		diagnosticGeoJSON = raw
		log.Printf("Experimental polygon land mask enabled: %s", maskPath)
		// Optional independent shoreline LINESTRING GeoJSON for the diagnostic. Not a land mask.
		coastPath := os.Getenv("SST_SHORELINE_GEOJSON")
		if coastPath == "" {
			coastPath = "data/coastline.geojson"
		}
		if buf, readErr := os.ReadFile(filepath.Clean(coastPath)); readErr == nil {
			var parsed struct {
				Type     string `json:"type"`
				Features []struct {
					Geometry struct {
						Type string `json:"type"`
					} `json:"geometry"`
				} `json:"features"`
			}
			if json.Unmarshal(buf, &parsed) == nil && parsed.Type == "FeatureCollection" && len(parsed.Features) > 0 {
				valid := true
				for _, feature := range parsed.Features {
					if feature.Geometry.Type != "LineString" && feature.Geometry.Type != "MultiLineString" {
						valid = false
						break
					}
				}
				if valid {
					shorelineGeoJSON = buf
					log.Printf("Dedicated shoreline diagnostic active: %s", coastPath)
				} else {
					log.Printf("Ignoring shoreline GeoJSON: geometries must be lines")
				}
			} else {
				log.Printf("Ignoring invalid shoreline GeoJSON: %s", coastPath)
			}
		}
	}
	log.Printf("Marine raster prototype v%s at http://%s", version, *addr)
	log.Fatal(http.ListenAndServe(*addr, handlerWithMask(endpoint, client, mask)))
}

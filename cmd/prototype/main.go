package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
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

const version = "0.4.20-N.2.4"

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

func finiteTransect(values ...float64) bool {
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
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
type cacheOnlyContextKey struct{}

func clientSSTContext(r *http.Request) context.Context {
	if r.URL.Query().Get("sst_network") == "cache" {
		return context.WithValue(r.Context(), cacheOnlyContextKey{}, true)
	}
	return r.Context()
}
func cacheOnly(ctx context.Context) bool { return ctx.Value(cacheOnlyContextKey{}) == true }
func gridDiskKey(key string) string      { return "display-grid-v1:" + key }
func readDisplayGrid(key string) (sst.Grid, bool) {
	path := cacheFile("display-sst", gridDiskKey(key))
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) > 14*24*time.Hour {
		return sst.Grid{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return sst.Grid{}, false
	}
	var grid sst.Grid
	if json.Unmarshal(raw, &grid) != nil || len(grid.Points) == 0 {
		return sst.Grid{}, false
	}
	return grid, true
}

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
	// Disk lookup precedes any upstream request. Cache-only clients can consume
	// files written by any other client, including after a server restart.
	if !force || cacheOnly(ctx) {
		if grid, ok := readDisplayGrid(key); ok {
			c.entries[key] = gridEntry{grid, time.Now()}
			c.mu.Unlock()
			return grid, "DISK-HIT", nil
		}
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
	// Reject missing data before allocating an upstream flight or slot.
	if cacheOnly(ctx) {
		c.mu.Unlock()
		return sst.Grid{}, "NOT-CACHED", fmt.Errorf("SST not cached; this browser is in Cache Only mode")
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
		// Persistent numerical grid cache, shared across all clients.
		// Write after unlocking to avoid blocking unrelated requests.
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
	if f.err == nil {
		writeAtomically(cacheFile("display-sst", gridDiskKey(key)), f.grid)
	}
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
	mux.HandleFunc("/api/detect-fronts", newFrontRequestCache().serve(store))
	// Research integration: front observations are loaded from an explicit local
	// GeoJSON file. Never synthesize observations or call NOAA for front lines.
	mux.HandleFunc("/api/research-fronts", func(w http.ResponseWriter, r *http.Request) {
		path := os.Getenv("FRONT_GEOJSON")
		if path == "" {
			http.Error(w, "Set FRONT_GEOJSON to the local research front-tracks.geojson", http.StatusNotFound)
			return
		}
		file, err := os.Open(path)
		if err != nil {
			http.Error(w, "Research front GeoJSON unavailable", http.StatusNotFound)
			return
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, 8<<20+1))
		if err != nil || len(data) > 8<<20 {
			http.Error(w, "Invalid or oversized front GeoJSON", http.StatusBadRequest)
			return
		}
		var fc struct {
			Type     string            `json:"type"`
			Features []json.RawMessage `json:"features"`
		}
		if json.Unmarshal(data, &fc) != nil || fc.Type != "FeatureCollection" {
			http.Error(w, "Expected a GeoJSON FeatureCollection", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/geo+json")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(data)
	})

	// Milestone M: strictly local L.4 reports. No live NOAA request to color fronts.
	mux.HandleFunc("/api/front-strength-data", func(w http.ResponseWriter, r *http.Request) {
		directory := os.Getenv("FRONT_VALIDATION_DIR")
		if directory == "" {
			http.Error(w, "Set FRONT_VALIDATION_DIR to the L.4 batch-validation folder", http.StatusNotFound)
			return
		}
		result := make(map[string][]map[string]string)
		for _, name := range []string{"validation", "transects"} {
			file, err := os.Open(filepath.Join(directory, name+".csv"))
			if err != nil {
				http.Error(w, "Missing L.4 "+name+" CSV", http.StatusNotFound)
				return
			}
			rows, err := csv.NewReader(io.LimitReader(file, 4<<20)).ReadAll()
			file.Close()
			if err != nil || len(rows) < 2 {
				http.Error(w, "Invalid L.4 CSV", http.StatusBadRequest)
				return
			}
			head := rows[0]
			var required []string
			if name == "validation" {
				required = []string{"date", "track_id", "front_id", "quality_flag", "transects_valid"}
			} else {
				required = []string{"date", "track_id", "front_id", "fraction_along_front", "measured_f_per_10nm", "transition_width_nm", "central_gradient_fraction", "status"}
			}
			for _, key := range required {
				found := false
				for _, col := range head {
					if col == key {
						found = true
						break
					}
				}
				if !found {
					http.Error(w, "Not an L.4 report: missing "+key, http.StatusBadRequest)
					return
				}
			}
			entries := make([]map[string]string, 0, len(rows)-1)
			for _, row := range rows[1:] {
				if len(row) != len(head) {
					http.Error(w, "Inconsistent L.4 report columns", http.StatusBadRequest)
					return
				}
				obj := make(map[string]string, len(head))
				for i, key := range head {
					obj[key] = row[i]
				}
				entries = append(entries, obj)
			}
			result[name] = entries
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})

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
		grid, cacheState, err := store.getDated(clientSSTContext(r), b, q.Get("refresh") == "1", day)
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
				grid, state, fetchErr := store.getDated(clientSSTContext(r), dataBounds, q.Get("refresh") == "1", day)
				if fetchErr != nil {
					// Browser navigation or date changes intentionally cancel obsolete tile requests.
					// Do not log these as upstream failures or return a misleading 502.
					if errors.Is(fetchErr, context.Canceled) || errors.Is(r.Context().Err(), context.Canceled) {
						return
					}
					if cacheOnly(clientSSTContext(r)) && state == "NOT-CACHED" {
						// Missing data in Cache Only mode is normal coverage information,
						// not an upstream failure. Do not spam the server log.
						w.Header().Set("X-SST-Cache", "NOT-CACHED")
						w.Header().Set("X-SST-No-Coverage", "cache-miss")
						w.WriteHeader(http.StatusNoContent)
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
	mux.HandleFunc("/api/sst-transect", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		lat, e1 := strconv.ParseFloat(q.Get("lat"), 64)
		lon, e2 := strconv.ParseFloat(q.Get("lon"), 64)
		east, e3 := strconv.ParseFloat(q.Get("east"), 64)
		north, e4 := strconv.ParseFloat(q.Get("north"), 64)
		day := q.Get("date")
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || !finiteTransect(lat, lon, east, north) || !validAnalysisDate(day) || day == "" {
			http.Error(w, "invalid transect parameters", 400)
			return
		}
		norm := math.Hypot(east, north)
		if norm < 0.9 || norm > 1.1 || math.Abs(lat) > 82 || math.Abs(lon) > 179 {
			http.Error(w, "invalid transect vector", 400)
			return
		}
		east /= norm
		north /= norm
		// One upstream numerical-grid request for a 20-NM transect.
		// Preserve missing observations; never substitute display gap-fill.
		const halfNM = 10.0
		dLat := halfNM / 60.0
		dLon := halfNM / (60.0 * math.Cos(lat*math.Pi/180))
		b := geo.Bounds{South: lat - dLat - 0.025, North: lat + dLat + 0.025, West: lon - dLon - 0.025, East: lon + dLon + 0.025}
		if b.West < -179.99 || b.East > 179.99 || b.South < -85 || b.North > 85 {
			http.Error(w, "transect outside data domain", 400)
			return
		}
		grid, _, err := store.getDated(clientSSTContext(r), b, false, day)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(r.Context().Err(), context.Canceled) {
				return
			}
			http.Error(w, "transect SST unavailable", 503)
			return
		}
		type sample struct {
			DistanceNM float64  `json:"distance_nm"`
			Celsius    *float64 `json:"celsius"`
		}
		points := make([]sample, 0, 21)
		for i := -10; i <= 10; i++ {
			distance := float64(i)
			y := lat + distance*north/60
			x := lon + distance*east/(60*math.Cos(lat*math.Pi/180))
			closest := math.MaxFloat64
			var temp *float64
			for _, pt := range grid.Points {
				separation := math.Hypot((pt.Lat - y), (pt.Lon-x)*math.Cos(y*math.Pi/180))
				if separation < closest {
					closest = separation
					v := pt.Temp
					temp = &v
				}
			}
			if closest > 0.016 {
				temp = nil
			}
			points = append(points, sample{DistanceNM: distance, Celsius: temp})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"samples": points, "timestamp": grid.Timestamp, "date": day, "center_lat": lat, "center_lon": lon, "method": "nearest_original_noaa_grid_sample", "sample_spacing_nm": 1})
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
		grid, _, err := store.getDated(clientSSTContext(r), geo.Bounds{West: west, East: east, South: south, North: north}, false, day)
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
		grid, cacheState, err := store.getDated(clientSSTContext(r), b, false, day)
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

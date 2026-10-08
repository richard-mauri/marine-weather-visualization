# Marine Raster Prototype v0.4.7 — conservative nearshore coverage estimates

Based on v0.4.3; numerical SST acquisition, cache, tile sizing, and geographic clipping unchanged.

- **SST mask edge** now draws a one-output-pixel magenta boundary from the alpha channel of the **fully composed** SST PNG, after segment rendering and masking. It therefore includes both shoreline clipping and source no-data edges. It cannot by itself distinguish them. It does not modify temperatures unless diagnostic mode is on. Browser interpolation may visually enlarge a one-pixel line when the map stretches an image.
- **Coastline diagnostic** now renders a thin cyan vector reference, separate from the raster. It uses the same local OSM-derived GeoJSON polygons for the mask, but independently vector-renders their boundaries in Leaflet. Polygon crop-border filtering is approximate; dedicated `data/coastline.geojson` lines remain preferable if available. Geometry is not globally complete.
- Enable both diagnostics at Tiburon to compare cyan polygon reference and magenta PNG transparency boundary. Disable SST mask edge for normal SST imagery. The cyan rectangles, if still visible as SST-colored blocks, are not necessarily mask boundaries.

## Install (verified archive root)

Stop the server. Extract into your home folder with `unzip -o marine-weather-visualization-v0.4.7.zip -d ~/`, then run `cd ~/marine-weather-visualization && go test ./... && go run ./cmd/prototype`. Confirm header v0.4.7. No `data/land.geojson` is included or overwritten. No production GitHub or Render changes are made.

## Limitations

Rendering uses JPL MUR coarse source data. Thin vector overlays may differ at steep coastal edges from raster pixel centers. Source missing values remain transparent; rectangular source-data interpolation regions may not be fixed by coastline masking. Browser/Safari visual acceptance remains pending.

---

# Marine Raster Prototype v0.4.3

Regional coastline-quality update based on v0.4.2. At close zoom levels, tiles render at 768×768 pixels for 1°–2° spans and 512×512 pixels for spans up to 4°. Broad views continue using 256×256 tiles. Pixel size is passed explicitly to the Go server and validated as 256, 512 or 768. Land-polygon masking and the mask-edge diagnostic use the same output-pixel coordinate transform and resolution; numerical SST samples remain those provided by JPL MUR. This improves *display edge precision* but does not improve the underlying dataset's nominal ~1 km measurement spacing.

**Known limitations:** Data around narrow Richardson Bay, Belvedere, and Tiburon still need visual acceptance testing. The clipped land polygons are unchanged and may show artificial rectangular boundaries in Coastline diagnostic. Dedicated shoreline-only GeoJSON is not included. Very high zoom levels may still show pixelation. Larger PNG responses at close zoom have higher CPU and memory cost. Existing `data/land.geojson` must remain in place; it is not bundled in the ZIP.

Run locally: `go test ./... && go run ./cmd/prototype`. Visit http://localhost:8081 and verify the heading reads v0.4.3. Compare SST and SST mask edge around Tiburon at zoom 9–12; compare the same view against v0.4.2 if possible. Observe request rates through `/api/sst-cache/status`. This prototype does not modify `pittsburg-saildata` or Render.

# Marine Raster Prototype v0.4.2

Reliability-focused update, keeping v0.4.1 SST rendering, longitude wrapping, latitude handling, and coastline processing unchanged.

The per-server grid cache already limited upstream concurrency to two and coalesced identical requests. v0.4.2 preserves that behavior while using request cancellation for fetch and retry waits, introducing jittered exponential backoff (up to three attempts) for HTTP 429/502/503, and a 20-second per-region cooldown after repeated transient failures. A stale cached grid may be served during cooldown or after an upstream failure. In-memory cache and cooldown are per Go process, not distributed between Render instances.

`GET /api/sst-cache/status` reports cache_entries, inflight, active_upstream, max_upstream, hits, misses, coalesced, stale_served, retries, transient_failures and cooldown_keys. Values reset on process restart. Cache misses or upstream failures are never presented as measured SST. NOAA may still return 503; this release reduces redundant retries rather than promising continuous availability. Retry-After headers are not yet parsed; cooldown is per request envelope rather than a global upstream circuit breaker.

Install: stop existing server, extract ZIP into `$HOME` (archive root `marine-weather-visualization/`), run `go test ./...` and `go run ./cmd/prototype`. The archive does not contain `data/land.geojson` and preserves locally generated coastline data. With the map open, inspect http://localhost:8081/api/sst-cache/status while panning. Production repo and Render are unchanged.

---

# Marine Raster Prototype v0.4.1

This revision extends SST rendering from the earlier ±80° display cutoff to ±85° latitude, within the usual Web Mercator practical limit (~85.05°). Tiles overlapping the cutoff (such as 80°–96°N) are retained and clipped to 80°–85°N; neither the SST raster nor its geographic overlay is stretched across the full 16°. Areas beyond 85° remain outside the display range. JPL MUR may also mask sea ice or lack valid samples.

The **SST tile grid** now has stable click-to-select details. Click any green or red tile rectangle to show the tile span, indices, status, coordinates, and clipped latitude range in the draggable SST status panel. The selection persists after the mouse leaves the tile and while the panel is moved. It updates when another rectangle is clicked, and clears if the selected tile is no longer in the current view. Hover tooltips have been removed.

This release preserves longitude wrapping, caching, SST mask/edge diagnostics, and the OpenStreetMap-derived regional shoreline configuration (`data/land.geojson` is not included in the ZIP). The Tiburon shoreline-mask mismatch remains unresolved.

## Install

Stop the current process, then extract the ZIP into your home directory (it contains the `marine-weather-visualization/` folder). From that folder run `go test ./...` and `go run ./cmd/prototype`. Refresh Safari. Confirm the header says v0.4.1.

## Test

Turn on SST and SST tile grid; pan north toward Greenland. Click a red tile or a partially clipped green tile in the 80°–96° latitude row. The details should remain readable after the pointer moves, and the valid 80°–85° portion should render where numerical data are available. Check SST tile counters; legitimate source no-data is not the same as a geographic exclusion.

# Marine Raster Prototype v0.4.0

This version adds longitude-wrapped SST tiles for continuous panning across ±180° and a draggable SST status panel. The browser keeps display-tile longitudes unwrapped, while the Go endpoint splits requests at the antimeridian and queries normalized coordinates. Cached numerical data is shared between repeated world copies via canonical geographic bounds. The on-map SST tile grid remains available for diagnostic validation.

Drag the **SST status** heading to reposition its panel. Double-click the heading to reset it to the lower left. The panel stays within the map on resize, and details remain collapsible and scrollable.

To install, extract the ZIP into your HOME directory and run `go test ./...` followed by `go run ./cmd/prototype` from `~/marine-weather-visualization`. Keep your locally generated `data/land.geojson` file; this archive does not include it.

Limitations: live ERDDAP/Safari verification is needed; this release does not correct Tiburon pixel-mask alignment or the optional shoreline-only diagnostic. SST is not a measurement of narrow Delta channels.

## v0.3.9 — out-of-range tile diagnostic

Adds an **SST tile grid** checkbox. At any zoom, it draws tile rectangles on the Leaflet map: green for tiles eligible for an SST request, red for tiles excluded by the existing client-side rules. The panel shows every excluded tile's span, indices, unwrapped longitude/latitude bounds, and precise exclusion reason. Hover over any rectangle to inspect it, and pan west across the Pacific gap. This is a **diagnostic-only release**: it does not change longitude wrapping, server tile validation, NOAA requests, masking, or caching. At global zoom, the map may display copies beyond ±180°, while the SST grid still applies a canonical-world exclusion rule. This overlay will show whether those are the tiles being skipped.

The SST grid currently appears only when SST is enabled. Green means requested/eligible, **not guaranteed loaded**; actual completed/failed counts remain in the status panel. Red rectangles extend beyond canonical longitude as map projections allow. The old coastline diagnostic is separate.

## v0.3.9 — Safari mask-edge checkbox fix

Fixed a browser-side exception: `URLSearchParams` was declared `const`, but code tried to append to it with `+=`. The diagnostic now uses `q.set("mask_edge", "1")`, which allows tile requests to run with SST mask edge enabled. No coastline polygons are included in the archive; preserve your existing `data/land.geojson`. This fixes the read-only-property failure but does not yet demonstrate pixel-perfect coast alignment.

# Marine Weather Visualization v0.3.7 — experimental

## v0.3.7: ERDDAP boundary and tile-edge correction

The numerical SST request now keeps Leaflet display-tile bounds unchanged while constraining NOAA ERDDAP query longitude to the interior of its coordinate axis (avoiding the observed `-180.000` HTTP 404). It also fetches one sample stride beyond adjacent tile boundaries where the dataset permits, reducing missing half-cell rows/columns at tile seams. Tests cover western/eastern longitude boundaries and overlapping tile requests. These bounds are conservative pending authoritative axis-metadata verification. **The tile seam correction is not yet visually confirmed in Safari.** The separate shoreline-only diagnostic remains future work.

Locally generated `data/land.geojson` is not included in the ZIP; it is retained when extracting over the existing project.


Standalone Go prototype; future import path `github.com/richard-mauri/marine-weather-visualization`. This does not modify `pittsburg-saildata`.

## Local usage

```sh
go test ./...
go run ./cmd/prototype
```

Open <http://localhost:8081>. Enable SST for numerical JPL MUR SST tiles. The numerical retrieval, multiresolution tiling, local caching, NOAA request throttling, color scale, and Leaflet interaction are preserved from v0.2.7.

## Coastline diagnostic and optional clipping

This release adds a **Coastline diagnostic** checkbox. It draws configured WGS84 GeoJSON **land polygons** as magenta outlines and translucent fill above the map. It also uses those polygons to clip SST PNG pixels in the Go renderer. It requires external geometry:

```sh
SST_LAND_GEOJSON=/absolute/path/to/validated-land-polygons.geojson go run ./cmd/prototype
```

The same file must be a GeoJSON FeatureCollection containing Polygon/MultiPolygon land geometry (with holes where appropriate), in lon/lat degrees. The server exposes `/api/coastline` to draw the exact file for visual comparison. The route returns 404 when no geometry is configured. The diagnostics **do not prove** the polygon quality or land/water correctness. Verify around Angel Island, Alcatraz, Golden Gate, Carquinez Strait and the Delta before using the mask in production.

**No high-resolution coastline dataset is bundled.** Download, inspect and license-check a suitable shoreline source (e.g. NOAA GSHHG full/high resolution); converting line features to valid closed land polygons is a separate geospatial processing step. Natural Earth 1:10m land may help test broad-scale alignment but is not sufficiently detailed to validate narrow Delta waterways.

## Known limitations

Satellite MUR SST does not give precise temperatures for Delta channels or resolve every coastal inlet. The fixed 5–30°C legend and large information panel remain. GeoJSON can be large and expensive to transmit and render; the diagnostic is intended for **regional** polygons. The optional land mask is global for the loaded geometry; land outside the supplied coverage will remain unmasked. Coastline datasets have not been verified in Safari in this release. SST must not be interpreted as an observation at every rendered pixel.

## Data sources and provenance

SST: JPL MUR via NOAA CoastWatch ERDDAP, `analysed_sst`. Map tiles: OpenStreetMap. Candidate shoreline dataset: NOAA GSHHG <https://www.ngdc.noaa.gov/mgg/shorelines/shorelines.html> (not included).


## v0.3.1: bundled experimental regional coastline

A **real GSHHS intermediate-resolution** land polygon dataset, extracted using Basemap's bundled GSHHS geometry, is provided as `cmd/prototype/coastline.geojson`. Its geographic envelope is longitude -123.6 to -120.3, latitude 36.7 to 39.0. This is **not** a verified high-resolution San Francisco Bay/Delta shoreline. Channels, estuaries, interior islands, and narrow water passages may be wrong. Do not use these contours for navigation.

The app loads this dataset automatically and masks SST pixels classified as land **inside that region**. It does not mask land outside the region. Check **Coastline diagnostic** to view the approximate boundaries in magenta. Use `SST_LAND_GEOJSON=/absolute/path/to/a/more-accurate-land.geojson go run ./cmd/prototype` to substitute verified GeoJSON. This dataset is provided for geospatial visualization testing only; the SST values remain approximately 1 km gridded analyses.

GSHHS is sourced from NOAA/NCEI's Global Self-consistent Hierarchical High-resolution Shorelines database (intermediate tier); map display coordinates use WGS84. The geometry in this build was extracted from the installed `mpl_toolkits.basemap_data` GSHHS intermediate dataset. The shoreline comparison against Leaflet has not yet been validated in Safari.


## v0.3.1 — coastline geometry safety and validation

The v0.2.9 screenshot confirmed that GSHHS intermediate land polygons cut straight across Tiburon/Richardson Bay and portions of the East Bay. **They are unsuitable for precision clipping**. Consequently, v0.3.1 does **not** automatically load this dataset. Numerical SST, server caching and multiresolution tiles continue to function, but may show source-grid land bleed. We deliberately avoid presenting a bad shoreline mask as accurate.

To enable a separately acquired, validated WGS84 GeoJSON `FeatureCollection` of **land polygons**, run:

```sh
SST_LAND_GEOJSON=/absolute/path/to/verified-land.geojson go run ./cmd/prototype
```

Use the **Coastline diagnostic** checkbox to examine magenta outlines around Tiburon, Angel Island, Alcatraz, the Oakland shoreline, and the Delta before relying on the mask. You may use `SST_USE_GSHHS_APPROX=1` only to compare the original coarse bundled geometry (not recommended for production). The new `/api/coastline/status` endpoint identifies whether a mask is loaded.

**High-resolution geometry remains a prerequisite**, not a delivered feature in this version. NOAA NGS shoreline datasets and OpenStreetMap-derived polygon data are candidates; external downloads were unavailable in the build environment. Do not use any SST visualization for navigation.


## v0.3.1 — local coastline-data workflow and diagnostic status

The GSHHS intermediate dataset shown in the earlier diagnostic is **not precise enough** for Bay shoreline clipping. No replacement shoreline data is included in this release. The prototype now automatically looks for `data/land.geojson` when started from the repository root. This must be a **verified** WGS84 GeoJSON `FeatureCollection` of land Polygon / MultiPolygon geometries. Alternatively set `SST_LAND_GEOJSON=/absolute/path/to/land.geojson`. Configure it before starting the server. The developer-only `SST_USE_GSHHS_APPROX=1` still explicitly opts in to the coarse sample.

The **Coastline diagnostic** checkbox is now disabled when no land geometry is configured; it will no longer flash on and off. The status panel explains how to activate it. Once verified polygons are installed, the checkbox can show the outlines, and the same geometry will be used by the SST mask. **Do not use unverified polygons for navigation.**

A dependable high-resolution source, proper coverage validation, and visual coastline acceptance tests are still required before claiming that SST clipping is solved. The existing NOAA data path, SST tiling and cache are unchanged.

## v0.3.3 — expanded Bay/Delta–Monterey coastline extraction

With your already-downloaded OSM source ZIP at `.coastline-source/land-polygons-split-4326.zip` and Docker Desktop running, regenerate the GeoJSON from the repository root:

```sh
bash scripts/prepare-coastline.sh
go test ./...
go run ./cmd/prototype
```

The script clips to WGS84 longitude **−123.60 to −120.50**, latitude **36.10 to 38.80**, extending south past Monterey and Point Sur while retaining San Francisco Bay and the Delta. The script uses `ghcr.io/osgeo/gdal:ubuntu-full-latest` with GEOS. It writes `data/land.geojson`, preserving the prior dataset as `data/land.geojson.previous`. This file is **not included** in the ZIP because it is generated locally from the global source archive. Do **not** overwrite an existing verified file unless you intend to regenerate it.

The diagnostic now uses `bbox` metadata to suppress magenta **strokes** along the rectangular crop boundary; the fill still reflects the clipped land polygon. Mask clipping continues at the region's coverage boundary, and SST beyond it remains subject to the source's own land mask. This is a visualization improvement, not independently validated shoreline accuracy; inspect Monterey, Point Sur, islands, Carquinez Strait and Delta channels. Existing caching, NOAA requests, and production project remain unchanged. OSM data © OpenStreetMap contributors, ODbL; see osmdata.openstreetmap.de for source license conditions.


## v0.3.3 improvements

- SST status panel can be collapsed using **Hide details**.
- Tile status distinguishes loaded, no-data, pending, failed, out-of-range and total candidate tiles. This exposes incomplete view coverage rather than treating cache reuse as proof of completeness.
- Geographic level-of-detail levels now include 64° and 128° at extreme zoom-out. These are coarse visualizations of numerical SST data, not added source resolution. Requests that cross ERDDAP geographic constraints can still fail; errors remain visible.
- Coastline diagnostic graphics automatically hide below Leaflet zoom 7 and reappear on zooming in if enabled. The underlying server-side mask is unchanged.
- Existing `data/land.geojson` on your Mac is **not** replaced by this ZIP. The Docker coastline script remains included.

The left-edge apparent missing coverage at continental scale may reflect missing upstream tiles, a geographic request limit, or incomplete view coverage. The enhanced counters should help distinguish those conditions in testing. Coastline crop boundaries may still be visible at regional zoom; separate coastline-only linework remains future work.


## v0.3.5 — global tile contract correction

The browser and Go server now accept the same SST tile spans (1, 2, 4, 8, 16, 32, 64, 128 degrees). Border tiles overlapping the supported [-180,180] longitude and [-80,80] latitude envelope are clipped consistently in both browser and server. Fully outside tiles remain unavailable; coverage may still be incomplete due to NOAA no-data or upstream errors. The coastline diagnostic skips long axis-aligned segments likely introduced by regional polygon cropping, but this is heuristic and is **not** an authoritative shoreline-only dataset. Locally generated `data/land.geojson` is preserved when unpacking this ZIP. No changes to the production repository.


## v0.3.5 — upstream diagnostics and independent shoreline input

Failed ERDDAP responses now log HTTP status, bounded response text and geographic bounds to the Go server terminal. Review these logs when global tiles fail with 404; this release **does not** assert that global coverage has been fixed. The server logs do not print full authenticated URLs. Failed tiles remain visibly counted rather than painted or silently suppressed.

Optional `data/coastline.geojson` (or `SST_SHORELINE_GEOJSON`) must contain a GeoJSON FeatureCollection whose geometries are **LineString** or **MultiLineString**. These are displayed as diagnostic magenta lines in preference to clipped polygon rings. The existing `data/land.geojson` remains authoritative for masking. If the dedicated shoreline file is absent, the diagnostic falls back to the imperfect polygon outlines. No new shoreline file is bundled: producing genuine uncropped linework requires a separate verified geographic source; deriving boundaries directly from clipped polygons would repeat the rectangular artifacts.

Inspect `http://localhost:8081/api/shoreline` to check if optional linework is configured. The previous `data/land.geojson` on your Mac is not included in this ZIP and remains in place on overwrite.

## v0.3.7 Tiburon SST edge diagnostic
The **SST mask edge** checkbox requests an overlay of magenta pixels along the water side of the **actual server-side land classification** on each numerical SST tile. Enable this with SST, turn off Coastline diagnostic (which shows the vector polygons), and inspect Tiburon, Belvedere and Richardson Bay. Then compare with Coastline diagnostic. This deliberately does not assert that an apparent mismatch has been fixed: the source grid has finite spacing, shoreline geometry may differ from the basemap, and diagnostic output is for visual verification. The edge overlay is produced only on demand and the underlying caching and grid source are unchanged. No land GeoJSON is bundled in the ZIP; your local data/land.geojson is retained.

## v0.4.7 — experimental nearshore coverage estimates

The previous strict pixel-footprint renderer preserves NOAA missing-data cells, which can create rectilinear SST transparency boundaries beside complex shorelines. With an OSM land polygon mask loaded, this release expands *only* existing valid raster pixels into nearby missing-data water pixels, with a maximum source-to-estimate displacement of 0.018° in each geographic direction. The expansion is confined to connected water pixels; every estimated target pixel is checked against OSM land polygons. Original NOAA sample colors and opacity are left untouched; **estimated pixels use alpha 150 instead of 220**. This is a nearest-valid-color rendering estimate, **not a new NOAA temperature observation** and not a substitute for higher-resolution SST. Areas farther from valid samples remain transparent. The fill is disabled if no land mask is loaded.

The magenta final-alpha boundary and cyan vector shoreline comparison remain available. The original acquisition caching and global tile logic are unchanged. Test especially around Richardson Bay, Tiburon, Berkeley and Alameda. Note: nearby pixels at large map spans may not represent fine shoreline data. No source data is edited.


## v0.4.7 — fixed-width mask-edge diagnostic

The magenta SST mask edge is now traced from the final PNG alpha in the browser and displayed as Leaflet vector line segments at a fixed one-screen-pixel stroke width, independent of map zoom. The actual SST image is unchanged. The separate cyan coastline diagnostic remains the reference. Segment extraction omits tile exterior borders and includes interior no-data boundaries. No new NOAA requests are required solely for the diagnostic.


## v0.4.7 coastline diagnostic cleanup

The cyan polygon-derived shoreline diagnostic now infers geographic crop bounds from polygon coordinates when older `data/land.geojson` files have no `bbox` property. Straight segments on the cropping rectangle are excluded even when the original source boundary has many shorter segments. This change affects only the cyan reference overlay. The SST renderer, nearshore estimate, alpha-edge diagnostic, and upstream caching remain unchanged.


## v0.4.8 — historical SST frames
The time control anchors a rolling 30-day window to the timestamp actually returned by NOAA. Selecting a prior day requests its ERDDAP analysis timestamp and isolates cache entries by analysis date. The fixed Celsius color scale is retained across playback. The server rejects responses whose date does not match the requested frame. Historical playback is not a forecast. The 2-request upstream limit still applies. If NOAA lacks a requested day, the tile displays an error rather than showing another day's observations.

## v0.4.9 — buffered historical playback (test candidate)

Historical SST now uses complete-frame browser buffering rather than clearing the map on every date change. The existing frame stays visible while new date-specific tiles download and decode; the application switches when the complete frame is ready. Up to 12 completed frames for the current map view are retained in a browser-side least-recently-used cache. Rewinding through retained dates should require no additional SST tile downloads. Playback opportunistically prefetches the following day. A manual map pan/zoom invalidates those browser buffers; Go's existing date-specific server cache continues to function as before. Speed settings govern the interval between **ready** frames; NOAA downloads can delay progress. The server-side SST rendering, nearshore interpolation, request limiter and cache are unchanged. This is a testing candidate; Safari/NOAA playback behavior still requires local verification.


## v0.4.11 — explicit historical SST rendering (test candidate)

The historical 30-day slider now **selects** a date without fetching SST data. Click **Render SST** to request and display the selected analysis. **Latest** only moves the selector to the most recent analysis and does not issue a request. The displayed date is labeled separately from the selected date. Automatic playback, speed controls, and background date prefetch are removed. The existing complete-frame swapping and bounded in-browser frame cache remain; map navigation still reloads the currently applied analysis for the new view. Existing SST masking, nearshore filling, NOAA request limits, and other diagnostics are unchanged.

## v0.4.11 tile-loading diagnostics

Historical SST remains an explicit-render workflow. The loading-progress counter reports prepared PNG tiles, and the optional Loading outlines overlay marks tile footprints amber while pending, green after successful decode, and red on failure. The currently displayed SST frame stays visible until the replacement is complete. Obsolete HTTP requests canceled by navigation or date changes are not reported as NOAA server failures.

## v0.4.13 — progressive historical SST rendering

The date slider continues to stage a date without issuing requests; **Render SST** starts loading it. Each validated and decoded tile is immediately placed on the map. Tiles for the previously displayed date remain underneath until the requested frame finishes, so the map is temporarily a mix of dates while loading. The SST status panel explicitly identifies this mixed-date condition. After a complete successful load, the prior frame is removed. On failure or cancellation, partially rendered new tiles are removed and the previously complete frame remains. The optional loading outlines and progress counter still indicate per-tile status. No automatic playback or prefetching was added.


## v0.4.13 — Stable historical SST controls

The two-row historical SST control uses reserved grid positions and fixed-size buttons, so selected-date and loading-progress text never reflows the slider or Render SST button. On narrow screens the timeline scrolls horizontally rather than wrapping. Progressive tile-by-tile SST rendering and explicit Render SST behavior remain unchanged.

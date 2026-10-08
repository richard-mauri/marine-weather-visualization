#!/usr/bin/env bash
set -euo pipefail
PROJECT="${1:-$HOME/marine-weather-visualization}"
ARCHIVE="$PROJECT/.coastline-source/land-polygons-split-4326.zip"
IMAGE=ghcr.io/osgeo/gdal:ubuntu-full-latest
[ -s "$ARCHIVE" ] || { echo "Missing $ARCHIVE; use your existing validated OSM ZIP download." >&2; exit 1; }
command -v docker >/dev/null || { echo 'Docker not available' >&2; exit 1; }
mkdir -p "$PROJECT/data"
# Crop extends beyond Monterey/Point Sur while retaining the Sacramento/Stockton Delta.
# These are dataset COVERAGE bounds, not guaranteed marine-data coverage.
WEST=-123.60; SOUTH=36.10; EAST=-120.50; NORTH=38.80
TMP="$PROJECT/data/land.v032.tmp.geojson"
trap 'rm -f "$TMP"' EXIT
# Select candidate global polygons using spatial index, then crop with GEOS.
docker run --rm -v "$PROJECT:/work" "$IMAGE"   ogr2ogr -overwrite -f GeoJSON /work/data/land.v032.tmp.geojson   /vsizip//work/.coastline-source/land-polygons-split-4326.zip/land-polygons-split-4326/land_polygons.shp   -spat "$WEST" "$SOUTH" "$EAST" "$NORTH"   -clipsrc "$WEST" "$SOUTH" "$EAST" "$NORTH"   -t_srs EPSG:4326 -nlt PROMOTE_TO_MULTI -lco RFC7946=YES
# Store coverage metadata used to hide artificial crop-border strokes in diagnostic.
python3 - "$TMP" "$PROJECT/data/land.geojson" "$WEST" "$SOUTH" "$EAST" "$NORTH" <<'PYINNER'
import json,os,sys
source,dest,*bounds=sys.argv[1:]
with open(source) as f: data=json.load(f)
assert data.get('type')=='FeatureCollection' and data.get('features'), 'No polygons returned'
data['bbox']=list(map(float,bounds))
tmp=dest+'.new'
with open(tmp,'w') as f:json.dump(data,f,separators=(',',':'))
if os.path.exists(dest):os.replace(dest,dest+'.previous')
os.replace(tmp,dest)
print('Wrote',dest,'features',len(data['features']),'bytes',os.path.getsize(dest))
PYINNER

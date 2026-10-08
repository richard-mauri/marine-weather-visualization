// Package sst declares an SST WMS endpoint and builds standards-compliant WMS 1.1.1 image requests.
// This adapter deliberately does not claim a particular NOAA layer is available until verified.
package sst

import (
	"fmt"
	"github.com/richard-mauri/marine-weather-visualization/geo"
	"net/url"
	"strconv"
	"strings"
)

type WMS struct{ Endpoint, Layer string }

func (w WMS) ImageURL(b geo.Bounds, width, height int) (string, error) {
	return w.LayerImageURL(b, width, height, w.Layer)
}

// LayerImageURL requests a WMS image for an explicit layer such as Land.
func (w WMS) LayerImageURL(b geo.Bounds, width, height int, layer string) (string, error) {
	if err := b.Validate(); err != nil {
		return "", err
	}
	if width < 1 || height < 1 || width > 4096 || height > 4096 {
		return "", fmt.Errorf("invalid image dimensions")
	}
	u, err := url.Parse(w.Endpoint)
	if err != nil || u == nil || u.Scheme != "https" || u.Host == "" {
		return "", fmt.Errorf("invalid HTTPS WMS endpoint")
	}
	if strings.TrimSpace(layer) == "" {
		return "", fmt.Errorf("WMS layer is required")
	}
	q := u.Query()
	q.Set("SERVICE", "WMS")
	q.Set("VERSION", "1.1.1")
	q.Set("REQUEST", "GetMap")
	q.Set("LAYERS", layer)
	q.Set("STYLES", "")
	q.Set("FORMAT", "image/png")
	q.Set("TRANSPARENT", "TRUE")
	q.Set("SRS", "EPSG:4326")
	q.Set("BBOX", strings.Join([]string{strconv.FormatFloat(b.West, 'f', 6, 64), strconv.FormatFloat(b.South, 'f', 6, 64), strconv.FormatFloat(b.East, 'f', 6, 64), strconv.FormatFloat(b.North, 'f', 6, 64)}, ","))
	q.Set("WIDTH", strconv.Itoa(width))
	q.Set("HEIGHT", strconv.Itoa(height))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

package sst

import (
	"github.com/richard-mauri/marine-weather-visualization/geo"
	"strings"
	"testing"
)

func TestWMS(t *testing.T) {
	w := WMS{"https://example.org/wms", "sst"}
	u, e := w.ImageURL(geo.Bounds{-123, 37, -122, 38}, 512, 512)
	if e != nil || !strings.Contains(u, "BBOX=-123.000000%2C37.000000%2C-122.000000%2C38.000000") {
		t.Fatalf("url=%s err=%v", u, e)
	}
}

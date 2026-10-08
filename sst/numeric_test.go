package sst

import (
	"github.com/richard-mauri/marine-weather-visualization/geo"
	"strings"
	"testing"
)

func TestParseGrid(t *testing.T) {
	g, e := ParseGrid(strings.NewReader("time,latitude,longitude,analysed_sst\n2026-10-07T00:00:00Z,37.8,-122.4,15.5\n2026-10-07T00:00:00Z,37.81,-122.39,NaN\n"))
	if e != nil || len(g.Points) != 1 || g.Timestamp != "2026-10-07T00:00:00Z" {
		t.Fatalf("grid %+v err %v", g, e)
	}
}
func TestRenderGrid(t *testing.T) {
	g := Grid{Points: []Point{{37.8, -122.4, 15.5}}}
	im := RenderGrid(g, geo.Bounds{West: -122.45, East: -122.35, South: 37.75, North: 37.85}, 100, 100)
	if im.Bounds().Dx() != 100 {
		t.Fatal("bad dimensions")
	}
}
func TestGridURL(t *testing.T) {
	u, e := BuildGridURL("https://example.org/erddap/griddap/jplMURSST41.csv0", geo.Bounds{West: -123, East: -122, South: 37, North: 38}, 180)
	if e != nil || !strings.Contains(u, "analysed_sst") {
		t.Fatalf("url %s %v", u, e)
	}
}

func TestGlobalMUREdgeRequests(t *testing.T) {
	endpoint := "https://example.org/erddap/griddap/jplMURSST41.csv0"
	for _, b := range []geo.Bounds{
		{West: -180, East: -160, South: 0, North: 32},
		{West: -180, East: -160, South: 64, North: 80},
		{West: 160, East: 180, South: 32, North: 64},
	} {
		u, err := BuildGridURL(endpoint, b, 180)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(u, "-180.000") || strings.Contains(u, "180.000") {
			t.Fatalf("request outside MUR longitude axis: %s", u)
		}
	}
}
func TestNeighboringRequestsHaveOverlappingHalos(t *testing.T) {
	endpoint := "https://example.org/erddap/griddap/jplMURSST41.csv0"
	a, err := BuildGridURL(endpoint, geo.Bounds{West: -128, East: -96, South: 0, North: 32}, 180)
	if err != nil {
		t.Fatal(err)
	}
	b, err := BuildGridURL(endpoint, geo.Bounds{West: -96, East: -64, South: 0, North: 32}, 180)
	if err != nil {
		t.Fatal(err)
	}
	// Both requests should include the shared -96 degree longitude within
	// the range rather than placing the last/first sample on disjoint sides.
	if !strings.Contains(a, "-95.820") || !strings.Contains(b, "-96.180") {
		t.Fatalf("no seam overlap: %s %s", a, b)
	}
}

func TestBuildGridURLAtDate(t *testing.T) {
	b := geo.Bounds{West: -123, East: -122, South: 37, North: 38}
	u, e := BuildGridURLAt("https://example.org/erddap/griddap/jplMURSST41.csv0", b, 180, "2026-09-25")
	if e != nil || !strings.Contains(u, "2026-09-25T09:00:00Z") {
		t.Fatalf("historical URL %q %v", u, e)
	}
	if _, e := BuildGridURLAt("https://example.org/erddap/griddap/jplMURSST41.csv0", b, 180, "not-a-day"); e == nil {
		t.Fatal("invalid date accepted")
	}
}

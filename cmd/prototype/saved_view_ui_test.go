package main

import (
	"os"
	"strings"
	"testing"
)

func TestSavedViewAndNauticalScaleUI(t *testing.T) {
	for _, file := range []string{"index.html", "../../web/index.html"} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		page := string(b)
		for _, needle := range []string{"id=\"save-map-view\"", "id=\"restore-map-view\"", "id=\"reset-map-view\"", "readSavedView()", "localStorage.setItem(MAP_VIEW_KEY", "localStorage.removeItem(MAP_VIEW_KEY", "setView([startupView.lat,startupView.lng]", "map.on('zoomend moveend resize',updateNauticalScale)", "map.distance(a,b)/1852", "id=\"nm-map-scale\""} {
			if !strings.Contains(page, needle) {
				t.Errorf("%s missing %q", file, needle)
			}
		}
	}
}

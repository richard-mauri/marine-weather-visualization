package main

import (
	"os"
	"strings"
	"testing"
)

func TestN13OnDemandInspectorAndDragHandle(t *testing.T) {
	data, err := os.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, fragment := range []string{
		"v0.4.20-N.2",
		"Show historical Baja research fronts",
		"wireSelectionControl(el,kind)",
		"el.setPointerCapture(pointer)",
		"Drag center to move, corner to resize",
		"line.on('click',e=>inspectResearchFront(f,e.latlng,true))",
		"function inspectResearchFront(feature,selectedLatLng,isOnDemand=false)",
		"const profileDay=isOnDemand?displayedFrontDay():p.date",
		"Inspect 20-NM SST transect",
		"hideDetectionBox();",
	} {
		if !strings.Contains(s, fragment) {
			t.Errorf("N1.3 UI missing %q", fragment)
		}
	}
	if strings.Contains(s, "line.bindPopup('<strong>On-demand") {
		t.Fatal("legacy popups still enabled")
	}
}

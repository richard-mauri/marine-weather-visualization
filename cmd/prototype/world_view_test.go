package main

import (
	"os"
	"strings"
	"testing"
)

// Regression check: global SST selection must not request wrapped world copies.
func TestWorldViewCanonicalLongitude(t *testing.T) {
	for _, name := range []string{"index.html", "../../web/index.html"} {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		for _, required := range []string{
			"map.getBoundsZoom(worldBounds,false)",
			"map.setMaxBounds(worldBounds)",
			"Math.max(-180,b.getWest())",
			"Math.min(180,b.getEast())",
			"worldMinZoom",
		} {
			if !strings.Contains(s, required) {
				t.Errorf("%s missing %q", name, required)
			}
		}
		if strings.Contains(s, "atWorldOverview()?-180:b.getWest()") {
			t.Errorf("%s still selects wrapped worlds", name)
		}
	}
}

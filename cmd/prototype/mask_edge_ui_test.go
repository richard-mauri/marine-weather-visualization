package main

import (
	"os"
	"strings"
	"testing"
)

// Ensure the mask-edge is vectorized from PNG alpha rather than burned into
// a raster tile. The old URL mask_edge query must not be used.
func TestMaskEdgeVectorOverlay(t *testing.T) {
	for _, path := range []string{"index.html", "../../web/index.html"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		html := string(raw)
		if !strings.Contains(html, "vectorAlphaEdge(blob,") {
			t.Errorf("%s missing client vector mask diagnostic", path)
		}
		if strings.Contains(html, "q.set('mask_edge','1')") {
			t.Errorf("%s still draws mask diagnostics in PNG", path)
		}
		if strings.Contains(html, "q+='&mask_edge=1'") {
			t.Errorf("%s has read-only binding assignment", path)
		}
	}
}

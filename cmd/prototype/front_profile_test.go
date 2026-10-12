package main

import (
	"math"
	"os"
	"strings"
	"testing"
)

func TestFrontTransectValidation(t *testing.T) {
	if !finiteTransect(32.5, -117.5, 1, 0) {
		t.Fatal("valid transect rejected")
	}
	if finiteTransect(math.NaN(), -117, 1, 0) {
		t.Fatal("NaN accepted")
	}
	if finiteTransect(33, math.Inf(1), 1, 0) {
		t.Fatal("infinity accepted")
	}
}
func TestFrontTransectUI(t *testing.T) {
	b, err := os.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"Inspect 20-NM SST transect", "/api/sst-transect?", "nearest-grid values"} {
		if !strings.Contains(string(b), s) {
			t.Errorf("missing %q", s)
		}
	}
}

package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResearchFrontGeoJSONEndpoint(t *testing.T) {
	h := handler("http://unused.invalid", &http.Client{})
	t.Setenv("FRONT_GEOJSON", "")
	req := httptest.NewRequest("GET", "/api/research-fronts", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("without local path: %d", rec.Code)
	}
	file := filepath.Join(t.TempDir(), "fronts.geojson")
	if err := os.WriteFile(file, []byte(`{"type":"FeatureCollection","features":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FRONT_GEOJSON", file)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"FeatureCollection"`) {
		t.Fatalf("read: %d %s", rec.Code, rec.Body.String())
	}
	if err := os.WriteFile(file, []byte(`{"type":"NotGeoJSON"}`), 0600); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid type: %d", rec.Code)
	}
}

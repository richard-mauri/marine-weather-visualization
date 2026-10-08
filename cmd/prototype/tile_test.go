package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTileValidationAndServerVersion(t *testing.T) {
	h := handler("https://example.org/test.csv0", http.DefaultClient)
	for _, tc := range []struct {
		url  string
		code int
	}{
		{"/api/sst-tile?span=3&x=-123&y=37", 400},
		{"/api/sst-tile?span=1&x=oops&y=37", 400},
		{"/api/sst-tile?span=4&x=0&y=22", 204},
		{"/api/health", 200},
	} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", tc.url, nil))
		if r.Code != tc.code {
			t.Errorf("%s: %d, want %d", tc.url, r.Code, tc.code)
		}
		if tc.url == "/api/health" && !strings.Contains(r.Body.String(), "0.4.4") {
			t.Errorf("health wrong version: %s", r.Body.String())
		}
	}
}

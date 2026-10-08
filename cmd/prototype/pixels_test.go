package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTilePixelSizeValidation(t *testing.T) {
	h := handler("https://example.org/test.csv0", http.DefaultClient)
	for _, tc := range []struct {
		query string
		code  int
	}{
		{"/api/sst-tile?span=1&x=0&y=90&pixels=256", 204},
		{"/api/sst-tile?span=1&x=0&y=90&pixels=768", 204},
		{"/api/sst-tile?span=1&x=0&y=0&pixels=300", 400},
		{"/api/sst-tile?span=1&x=0&y=0&pixels=bad", 400},
	} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", tc.query, nil))
		if r.Code != tc.code {
			t.Errorf("%s got %d want %d", tc.query, r.Code, tc.code)
		}
	}
}

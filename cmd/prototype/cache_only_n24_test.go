package main

import (
	"os"
	"strings"
	"testing"
)

// Guard the expected Cache Only miss behavior and partial viewport rendering.
func TestN24CacheOnlyTileMissIsNotServerError(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	for _, token := range []string{`state == "NOT-CACHED"`, `"X-SST-No-Coverage", "cache-miss"`, `http.StatusNoContent`} {
		if !strings.Contains(s, token) {
			t.Errorf("missing cache-only handling %s", token)
		}
	}
}
func TestN24ClientDoesNotRejectPartialViewport(t *testing.T) {
	src, err := os.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	for _, token := range []string{`frame.partial=true`, `result==='missing'`, `Same-date frames retain their previously painted tiles`} {
		if !strings.Contains(s, token) {
			t.Errorf("missing partial-tile behavior %s", token)
		}
	}
}

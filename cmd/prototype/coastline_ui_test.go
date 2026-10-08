package main

import (
 "os"
 "strings"
 "testing"
)

func TestCoastlineFallbackCropBounds(t *testing.T) {
 for _, path := range []string{"index.html", "../../web/index.html"} {
  raw, err := os.ReadFile(path)
  if err != nil { t.Fatal(err) }
  code := string(raw)
  for _, fragment := range []string{"function inferBounds()", "const inferred=inferBounds()", "data.bbox", "function onCropEdge(a,b)", "for(const ring of rings)strokeRing(ring)"} {
   if !strings.Contains(code,fragment) { t.Errorf("%s missing %q",path,fragment) }
  }
 }
}

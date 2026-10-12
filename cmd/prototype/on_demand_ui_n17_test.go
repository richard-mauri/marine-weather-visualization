package main

import (
 "os"
 "strings"
 "testing"
)

func TestSelectAreaAlwaysUsesCurrentViewport(t *testing.T) {
 for _, filename := range []string{"index.html", "../../web/index.html"} {
  b,err:=os.ReadFile(filename);if err!=nil{t.Fatal(err)}
  s:=string(b)
  required:=[]string{
   "v0.4.20-N.2",
   "let selectionMapNavigating=false, selectionViewportPoint=null;",
   "const viewportCenter=map.getCenter();",
   "savedBoxCenter=L.latLng(viewportCenter.lat,viewportCenter.lng);",
   "updateDetectionBox(savedBoxCenter);",
   "selectionMapNavigating=false;selectionViewportPoint=null;",
  }
  for _,x:=range required {if !strings.Contains(s,x){t.Errorf("%s missing %q",filename,x)}}
 }
}

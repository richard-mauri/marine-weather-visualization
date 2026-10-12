package main

import (
 "os"
 "strings"
 "testing"
)

func TestN16ViewportAnchoredSelection(t *testing.T) {
 b,err:=os.ReadFile("index.html")
 if err!=nil {t.Fatal(err)}
 s:=string(b)
 for _,want:=range []string{
  "selectionViewportPoint=null", 
  "map.on('movestart'",
  "map.on('move'",
  "map.on('moveend'",
  "map.containerPointToLatLng(selectionViewportPoint)",
  "if(!selectionMapNavigating)selectionViewportPoint=map.latLngToContainerPoint(savedBoxCenter)",
  "const b=selectedArea,west=b.getWest()",
 } {if !strings.Contains(s,want){t.Errorf("missing viewport selection behavior %q",want)}}
}

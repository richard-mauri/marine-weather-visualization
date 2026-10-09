package main

import (
 "os"
 "strings"
 "testing"
)

func TestInitialSSTAndUnifiedRenderUI(t *testing.T) {
 for _, path := range []string{"index.html", "../../web/index.html"} {
  b,err:=os.ReadFile(path);if err!=nil {t.Fatal(err)}
  s:=string(b)
  if !strings.Contains(s,"map.whenReady(()=>setTimeout(()=>{if(toggle.checked&&!visibleFrame&&!frameController)refresh(false);},400));") {t.Errorf("%s missing startup render",path)}
  if strings.Contains(s,"Refresh SST") || strings.Contains(s,"id=\"reload\"") {t.Errorf("%s still has redundant refresh control",path)}
  if !strings.Contains(s,"id=\"time-render\"") {t.Errorf("%s missing date-specific render control",path)}
 }
}

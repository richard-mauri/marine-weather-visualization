package sst

import "testing"

func TestConnectFrontPaths(t *testing.T) {
	s := []BoundarySegment{
		{X0: 0, Y0: 0, X1: 1, Y1: 0, Contrast10NMF: 2},
		{X0: 1, Y0: 0, X1: 2, Y1: 0, Contrast10NMF: 4},
		{X0: 7, Y0: 3, X1: 8, Y1: 3, Contrast10NMF: 5},
	}
	p, st := ConnectFrontPaths(s, 30, 0.03, 2.8)
	if len(p) != 1 || p[0].Segments != 2 || st.RejectedShort != 1 {
		t.Fatalf("paths=%+v stats=%+v", p, st)
	}
	if p[0].MeanContrast10NMF != 3 || p[0].MaxContrast10NMF != 4 {
		t.Fatalf("contrast %+v", p[0])
	}
}
func TestConnectFrontPathsDoesNotBridgeGap(t *testing.T) {
	s := []BoundarySegment{{X0: 0, Y0: 0, X1: 1, Y1: 0}, {X0: 1.1, Y0: 0, X1: 2.1, Y1: 0}}
	p, _ := ConnectFrontPaths(s, 30, .03, 0)
	if len(p) != 2 {
		t.Fatalf("joined across a gap: %v", p)
	}
}
func TestConnectFrontPathsStopsAtBranch(t *testing.T) {
	s := []BoundarySegment{{X0: 0, Y0: 0, X1: 1, Y1: 0}, {X0: 1, Y0: 0, X1: 2, Y1: 0}, {X0: 1, Y0: 0, X1: 1, Y1: 1}}
	p, st := ConnectFrontPaths(s, 30, .03, 0)
	if len(p) != 3 || st.RejectedBranchEndpoints != 1 {
		t.Fatalf("paths=%d stats=%+v", len(p), st)
	}
}

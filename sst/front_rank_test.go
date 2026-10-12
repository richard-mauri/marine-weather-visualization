package sst

import "testing"

func TestRankFrontPaths(t *testing.T) {
	p := []FrontPath{{Points: []FrontPoint{{0, 0}, {10, 0}}, LengthNM: 12, MeanContrast10NMF: 2, MaxContrast10NMF: 3}, {Points: []FrontPoint{{0, 10}, {10, 10}}, LengthNM: 4, MeanContrast10NMF: 9}, {Points: []FrontPoint{{0, .2}, {10, .2}}, LengthNM: 11, MeanContrast10NMF: 1.5}}
	got := RankFrontPaths(p, 10)
	if len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
	if got[0].ID != "F001" || got[1].PotentialDuplicateOf != "F001" {
		t.Fatalf("unexpected ranking %+v", got)
	}
}
func TestNoFalseDuplicate(t *testing.T) {
	a := FrontPath{Points: []FrontPoint{{0, 0}, {10, 0}}, LengthNM: 12}
	b := FrontPath{Points: []FrontPoint{{0, 3}, {10, 3}}, LengthNM: 12}
	if frontsOverlap(a, b) {
		t.Fatal("distinct fronts flagged")
	}
}

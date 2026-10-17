package domain

import (
	"fmt"
	"testing"
)

func TestPrerankOrdersByQuality(t *testing.T) {
	good := Candidate{Return1Y: 40, Vol: 15, Liquidity: "high"}
	bad := Candidate{Return1Y: -10, Vol: 30, Liquidity: "low"}
	if Prerank(good, 6) <= Prerank(bad, 6) {
		t.Errorf("good %.1f should beat bad %.1f", Prerank(good, 6), Prerank(bad, 6))
	}
	if v := Prerank(Candidate{Return1Y: 10, Liquidity: "high"}, 6); v < 0 || v > 100 {
		t.Errorf("zero vol out of range: %.1f", v)
	}
}

func TestShortlistKeepsTop20Deterministically(t *testing.T) {
	var in []Candidate
	for i := 0; i < 35; i++ {
		in = append(in, Candidate{Symbol: fmt.Sprintf("S%02d", i), Return1Y: float64(i), Vol: 20, Liquidity: "high"})
	}
	keep := shortlist(append([]Candidate(nil), in...), 6)
	if len(keep) != ShortlistSize {
		t.Fatalf("kept %d", len(keep))
	}
	if keep[0].Symbol != "S34" || keep[ShortlistSize-1].Symbol != "S15" {
		t.Errorf("unexpected order %s .. %s", keep[0].Symbol, keep[ShortlistSize-1].Symbol)
	}
	if got := shortlist(in[:5], 6); len(got) != 5 {
		t.Errorf("small class must pass through, got %d", len(got))
	}
}

func TestMatchScoresOnlyShortlist(t *testing.T) {
	var cands []Candidate
	for i := 0; i < 30; i++ {
		c := cand(fmt.Sprintf("LC%02d", i), ClassLargeCap, fmt.Sprintf("sec%d", i%7), float64(i))
		cands = append(cands, c)
	}
	out := Match(base(), cands, nil, 6, MacroSignals{})
	for _, s := range out.Trace.Shortlist {
		if s.Class == ClassLargeCap && (s.Eligible != 30 || s.Kept != ShortlistSize) {
			t.Errorf("shortlist trace %+v", s)
		}
	}
	n := 0
	for k := range out.Trace.ScoresPerAsset {
		if len(k) >= 2 && k[:2] == "LC" {
			n++
		}
	}
	if n != ShortlistSize {
		t.Errorf("scored %d, want %d", n, ShortlistSize)
	}
}

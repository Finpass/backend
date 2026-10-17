package domain

import "testing"

func TestCompareRanksAndGates(t *testing.T) {
	p := base()
	p.Sharia = "yes"
	a := cand("A", ClassLargeCap, "Financials", 20)
	b := cand("B", ClassLargeCap, "Energy", 8)
	c := cand("C", ClassLargeCap, "Energy", 30)
	c.Sharia = "non_sharia"
	out := Compare(p, []Candidate{b, a, c}, 4, MacroSignals{})
	if len(out.Assets) != 3 || out.Assets[0].Symbol != "B" {
		t.Fatalf("caller order kept: %+v", out.Assets)
	}
	if out.Best != "A" || out.Assets[1].Rank != 1 || out.Assets[0].Rank != 2 {
		t.Errorf("best %q ranks %d %d", out.Best, out.Assets[1].Rank, out.Assets[0].Rank)
	}
	if g := out.Assets[2]; g.Gate.Eligible || g.Gate.Gate != "gate_sharia" || g.Rank != 0 {
		t.Errorf("sharia gate: %+v rank %d", g.Gate, g.Rank)
	}
	var ret *Highlight
	for i := range out.Highlights {
		if out.Highlights[i].Key == "return" {
			ret = &out.Highlights[i]
		}
	}
	if ret == nil || ret.Symbol != "C" {
		t.Errorf("return leader should be C (highest, even when gated out): %+v", out.Highlights)
	}
}

func TestCompareNoHighlightWhenEqual(t *testing.T) {
	out := Compare(base(), []Candidate{cand("A", ClassLargeCap, "X", 10), cand("B", ClassLargeCap, "Y", 10)}, 4, MacroSignals{})
	if len(out.Highlights) != 0 {
		t.Errorf("identical figures have no leader: %+v", out.Highlights)
	}
}

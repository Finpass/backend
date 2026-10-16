package domain

import (
	"fmt"
	"math"
	"sort"
)

type rng struct{ Lo, Hi float64 }

func (r rng) mid() float64 { return (r.Lo + r.Hi) / 2 }

// base_allocation of the YAML (percent).
var baseAllocation = map[string]map[string]rng{
	Conservative: {"equity": {20, 35}, "fixed_income": {35, 50}, "cash": {15, 25}, "gold": {5, 10}, "crypto": {0, 0}},
	Moderate:     {"equity": {45, 65}, "fixed_income": {20, 30}, "cash": {5, 10}, "gold": {5, 10}, "crypto": {0, 5}},
	Aggressive:   {"equity": {70, 90}, "fixed_income": {5, 15}, "cash": {0, 5}, "gold": {0, 10}, "crypto": {0, 10}},
}

// equitySplit divides the equity bucket per profile. The YAML only gives 0-100% ranges per class; this split is an engine choice.
var equitySplit = map[string][4]float64{ // large, mid/small, global, etf
	Conservative: {0.60, 0, 0.10, 0.30},
	Moderate:     {0.45, 0.15, 0.15, 0.25},
	Aggressive:   {0.35, 0.20, 0.25, 0.20},
}

// AllocationTrace documents layer 1.
type AllocationTrace struct {
	Profile    string             `json:"profile"`
	Bucket     map[string]float64 `json:"bucket"`
	Modifiers  []string           `json:"modifiers"`
	Target     map[string]float64 `json:"target"`
	Investable bool               `json:"investable"`
}

// TargetAllocation implements layer 1. Result values are percent and sum to 100 (0.1 resolution).
func TargetAllocation(p Profile) AllocationTrace {
	tr := AllocationTrace{Profile: p.Risk, Modifiers: []string{}, Investable: p.Ladder > 2}
	base, ok := baseAllocation[p.Risk]
	if !ok {
		base = baseAllocation[Moderate]
		tr.Modifiers = append(tr.Modifiers, "unknown risk profile: Moderate base used")
	}
	b := map[string]float64{}
	for k, r := range base {
		b[k] = r.mid()
	}
	tr.Bucket = map[string]float64{}
	mod := func(s string, a ...any) { tr.Modifiers = append(tr.Modifiers, fmt.Sprintf(s, a...)) }
	shift := func(from, to string, pts float64) float64 {
		pts = math.Min(pts, b[from])
		b[from] -= pts
		b[to] += pts
		return pts
	}
	if p.Ladder <= 2 {
		mod("ladder_level <= 2: no investment allocation, only deposits, money-market mutual fund and government bond")
		t := map[string]float64{ClassDeposit: 50, ClassFund: 30, ClassBondGov: 20}
		if p.Sharia == "yes" {
			t = map[string]float64{ClassDeposit: 50, ClassFund: 30, ClassSukuk: 20}
			mod("q22 = yes: government bond replaced by sukuk")
		}
		tr.Target = t
		for k, r := range base {
			tr.Bucket[k] = r.mid()
		}
		return tr
	}
	if p.HorizonYears() < 3 {
		pts := shift("equity", "fixed_income", b["equity"]-base["equity"].Lo)
		mod("horizon < 3 years: equity to lower bound %.0f%% (%.1f points to fixed income)", base["equity"].Lo, pts)
	}
	if p.Employment == "freelancer" || p.Employment == "business_owner" {
		mod("q10 = %s: %.1f points equity to cash", p.Employment, shift("equity", "cash", 10))
	}
	if p.Dependents >= 3 {
		mod("q5 >= 3: %.1f points equity to fixed income", shift("equity", "fixed_income", 5))
	}
	if p.Knowledge == "none" || p.Knowledge == "basic" {
		if b["crypto"] > 0 {
			mod("q16 = %s: crypto excluded, %.1f points to fixed income", p.Knowledge, shift("crypto", "fixed_income", b["crypto"]))
		}
	}
	for k, v := range b {
		tr.Bucket[k] = round1(v)
	}
	t := map[string]float64{}
	sp := equitySplit[p.Risk]
	if !ok {
		sp = equitySplit[Moderate]
	}
	t[ClassLargeCap], t[ClassMidSmall], t[ClassGlobal], t[ClassETF] = b["equity"]*sp[0], b["equity"]*sp[1], b["equity"]*sp[2], b["equity"]*sp[3]
	if p.Knowledge == "none" || p.Knowledge == "basic" { // gate_knowledge: no small cap for q16 none/basic
		t[ClassLargeCap] += t[ClassMidSmall]
		mod("q16 = %s: small-cap excluded, share moved to large cap", p.Knowledge)
		t[ClassMidSmall] = 0
	}
	if p.Sharia == "yes" {
		t[ClassSukuk] = b["fixed_income"]
		mod("q22 = yes: government bond replaced by sukuk")
	} else {
		t[ClassBondGov] = b["fixed_income"]
	}
	t[ClassDeposit], t[ClassFund] = b["cash"]*0.6, b["cash"]*0.4
	t[ClassGold], t[ClassCrypto] = b["gold"], b["crypto"]
	mod("q24 (global exposure) is not part of questionnaire v4: global equity is kept")
	tr.Target = normalise(t)
	return tr
}

// normalise scales to 100 and rounds to 0.1 with the residual on the largest class.
func normalise(t map[string]float64) map[string]float64 {
	sum := 0.0
	for _, v := range t {
		sum += v
	}
	out := map[string]float64{}
	if sum == 0 {
		return out
	}
	keys := sortedKeys(t)
	total := 0.0
	big := ""
	for _, k := range keys {
		if t[k] <= 0 {
			continue
		}
		out[k] = round1(t[k] / sum * 100)
		total += out[k]
		if big == "" || out[k] > out[big] {
			big = k
		}
	}
	if big != "" {
		out[big] = round1(out[big] + 100 - total)
	}
	return out
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

func sortedKeys[V any](m map[string]V) []string {
	k := make([]string, 0, len(m))
	for s := range m {
		k = append(k, s)
	}
	sort.Strings(k)
	return k
}

var classOrder = []string{ClassLargeCap, ClassMidSmall, ClassGlobal, ClassETF, ClassBondGov, ClassSukuk, ClassFund, ClassDeposit, ClassGold, ClassCrypto, ClassProperty}

func orderedClasses(t map[string]float64) []string {
	var out []string
	for _, c := range classOrder {
		if t[c] > 0 {
			out = append(out, c)
		}
	}
	return out
}

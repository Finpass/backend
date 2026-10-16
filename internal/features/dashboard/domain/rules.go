package domain

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

var riskLevels = map[string]int{"Conservative": 1, "Moderate": 2, "Aggressive": 3}

func RiskLevel(p string) int { return riskLevels[p] }

type tier struct {
	stocks, bonds, cash float64
}

// Fixed allocation per final profile (rules-only fallback until the matching engine provides one).
var tiers = map[string]tier{
	"Conservative": {stocks: 15, bonds: 50, cash: 35},
	"Moderate":     {stocks: 50, bonds: 35, cash: 15},
	"Aggressive":   {stocks: 70, bonds: 20, cash: 10},
}

// RulesRecommendation derives a simple allocation text from the final profile. No assessment: empty text and allocation.
func RulesRecommendation(p Profile, now time.Time) Recommendation {
	r := Recommendation{Allocation: []Allocation{}, Source: "rules", GeneratedAt: now.UTC().Format(time.RFC3339)}
	t, ok := tiers[p.RiskProfile]
	if !ok {
		return r
	}
	sharia := p.Sharia == "yes"
	bondID, bondEn, stockID, stockEn := "obligasi", "bonds", "saham", "stocks"
	if sharia {
		bondID, bondEn, stockID, stockEn = "sukuk", "sukuk", "saham syariah", "sharia stocks"
	}
	r.Allocation = []Allocation{{Label: strings.ToUpper(stockID[:1]) + stockID[1:], Pct: t.stocks}, {Label: strings.ToUpper(bondID[:1]) + bondID[1:], Pct: t.bonds}, {Label: "Kas", Pct: t.cash}}
	r.TextID = fmt.Sprintf("Berdasarkan profil risiko %s Anda, alokasi yang disarankan adalah %.0f%% ke %s, %.0f%% ke %s, dan %.0f%% ke kas untuk keseimbangan risiko dan imbal hasil yang optimal.",
		p.RiskProfile, t.stocks, stockID, t.bonds, bondID, t.cash)
	r.TextEn = fmt.Sprintf("Based on your %s risk profile, the suggested allocation is %.0f%% in %s, %.0f%% in %s and %.0f%% in cash for a balance of risk and return.",
		p.RiskProfile, t.stocks, stockEn, t.bonds, bondEn, t.cash)
	return r
}

// group maps an asset class code to an allocation group.
func group(class string) string {
	switch class {
	case "equity", "etf":
		return "stocks"
	case "bond_sbn":
		return "bonds"
	case "mutual_fund":
		return "funds"
	case "gold":
		return "gold"
	case "crypto":
		return "crypto"
	}
	return "cash"
}

var groupLabels = map[string][2]string{
	"stocks": {"Stocks", "Saham"}, "bonds": {"Bonds", "Obligasi"}, "funds": {"Mutual funds", "Reksa Dana"},
	"gold": {"Gold", "Emas"}, "crypto": {"Crypto", "Kripto"}, "cash": {"Cash & other", "Kas & Lainnya"},
	"selfreported": {"Investment assets", "Aset Investasi"},
}

// Allocate splits total assets (savings + holdings, or savings + self-reported investments when there are no holdings)
// into class groups, in percent with one decimal, summing to exactly 100 (largest remainder). Empty total: no slices.
func Allocate(holdings []Holding, savings, selfReported int64) []Slice {
	amounts := map[string]float64{}
	amounts["cash"] += float64(savings)
	if len(holdings) == 0 {
		amounts["selfreported"] += float64(selfReported)
	}
	for _, h := range holdings {
		amounts[group(h.AssetClass)] += h.Value
	}
	var total float64
	for _, v := range amounts {
		total += v
	}
	if total <= 0 {
		return []Slice{}
	}
	type part struct {
		key    string
		tenths int
		frac   float64
	}
	var parts []part
	sum := 0
	for k, v := range amounts {
		if v <= 0 {
			continue
		}
		x := v / total * 1000
		t := int(math.Floor(x))
		parts = append(parts, part{k, t, x - float64(t)})
		sum += t
	}
	sort.Slice(parts, func(i, j int) bool {
		if parts[i].frac != parts[j].frac {
			return parts[i].frac > parts[j].frac
		}
		return parts[i].key < parts[j].key
	})
	for i := 0; sum < 1000 && i < len(parts); i++ {
		parts[i].tenths++
		sum++
	}
	sort.Slice(parts, func(i, j int) bool {
		if parts[i].tenths != parts[j].tenths {
			return parts[i].tenths > parts[j].tenths
		}
		return parts[i].key < parts[j].key
	})
	out := make([]Slice, len(parts))
	for i, p := range parts {
		l := groupLabels[p.key]
		out[i] = Slice{Label: l[0], LabelID: l[1], Pct: float64(p.tenths) / 10}
	}
	return out
}

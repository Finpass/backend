package domain

import (
	"math"
	"time"
)

const macroThreshold = 60.0

type MacroPick struct {
	Indicator string   `json:"indicator"`
	Label     Text     `json:"label"`
	ValueText string   `json:"value,omitempty"`
	Change    *float64 `json:"change,omitempty"`
	Headline  string   `json:"headline,omitempty"`
	Reason    Text     `json:"relevanceReason"`
	Score     float64  `json:"score"`
	Source    string   `json:"source"`
}

type MacroTrace struct {
	Indicator string  `json:"indicator"`
	Score     float64 `json:"score"`
	Relevant  bool    `json:"relevant"`
	Decision  string  `json:"decision"`
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func relevance(p Profile, hs []Holding, key string) (bool, Text) {
	fixed, equity, foreign := false, false, false
	for _, h := range hs {
		switch h.Class {
		case ClassBondGov, ClassSukuk, ClassFund, ClassDeposit:
			fixed = true
		case ClassLargeCap, ClassMidSmall, ClassGlobal, ClassETF:
			equity = true
		}
		if h.Class == ClassGlobal || h.Class == ClassCrypto || (h.Currency != "" && h.Currency != "IDR") {
			foreign = true
		}
	}
	switch key {
	case "bi_rate":
		return fixed, Text{"Anda punya instrumen pendapatan tetap.", "You hold fixed-income instruments."}
	case "inflation":
		return p.Goal == "emergency_fund", Text{"Tujuan utama Anda adalah dana darurat.", "Your main goal is an emergency fund."}
	case "usd_idr":
		return foreign, Text{"Anda punya aset dalam mata uang asing.", "You hold foreign-currency assets."}
	case "gdp":
		return equity, Text{"Anda punya saham atau ETF.", "You hold equities or ETFs."}
	}
	return false, Text{}
}

// MatchMacro implements daily layer 3. today is the user's local date; releases older than one day are not eligible.
func MatchMacro(p Profile, hs []Holding, ind []MacroIndicator, today time.Time) (*MacroPick, []MacroTrace) {
	var best *MacroPick
	var tr []MacroTrace
	td := dayStart(today)
	for _, m := range ind {
		age := int(td.Sub(dayStart(m.AsOf)).Hours() / 24)
		t := MacroTrace{Indicator: m.Key}
		if age < 0 || age > 1 {
			t.Decision = "no release within the last day"
			tr = append(tr, t)
			continue
		}
		mag := 70.0
		if m.Change != nil {
			mag = math.Min(100, math.Abs(*m.Change)/2*100)
			if m.Key == "usd_idr" && math.Abs(*m.Change) <= 2 {
				t.Decision = "usd_idr change <= 2%"
				tr = append(tr, t)
				continue
			}
		}
		ok, why := relevance(p, hs, m.Key)
		rel := 0.0
		if ok {
			rel = 100
		}
		rec := 100.0
		if age == 1 {
			rec = 70
		}
		t.Score, t.Relevant = round1(mag*0.40+rel*0.40+rec*0.20), ok
		switch {
		case !ok:
			t.Decision = "not relevant to this user"
		case t.Score < macroThreshold:
			t.Decision = "below threshold 60"
		default:
			t.Decision = "candidate"
			if best == nil || t.Score > best.Score {
				best = &MacroPick{Indicator: m.Key, Label: m.Label, ValueText: m.ValueText, Change: m.Change, Headline: m.Headline, Reason: why, Score: t.Score, Source: m.Source}
			}
		}
		tr = append(tr, t)
	}
	if best != nil {
		for i := range tr {
			if tr[i].Indicator == best.Indicator && tr[i].Decision == "candidate" {
				tr[i].Decision = "chosen"
			}
		}
	}
	return best, tr
}

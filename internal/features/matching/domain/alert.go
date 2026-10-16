package domain

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Default thresholds (percent) per class from the Asset Classifier YAML; used when a holding has no stored value.
var defaultThreshold = map[string]float64{
	ClassLargeCap: 3, ClassMidSmall: 5, ClassGlobal: 3, ClassETF: 3, ClassFund: 2, ClassGold: 2, ClassCrypto: 5, ClassProperty: 3, "commodity": 5,
}

const (
	AlertPrice = "asset_alert"
	AlertCorp  = "corporate_action"
	Alert52w   = "52w_high_low"
)

type Alert struct {
	Type        string  `json:"type"`
	Asset       string  `json:"asset"`
	Name        string  `json:"name"`
	ChangePct   float64 `json:"change"`
	Direction   string  `json:"direction"` // up | down
	PositionIDR int64   `json:"positionChangeIDR"`
	Detail      string  `json:"detail,omitempty"` // high | low | dividend
	Amount      float64 `json:"amountPerShare,omitempty"`
	Repeated    bool    `json:"repeated"`
	Key         string  `json:"key"`
}

type AlertTrace struct {
	Asset     string  `json:"asset"`
	ChangePct float64 `json:"changePct"`
	Threshold float64 `json:"thresholdPct"`
	Triggered bool    `json:"triggered"`
	Note      string  `json:"note,omitempty"`
}

// MatchAlert implements daily layer 1. Strictly greater than the threshold triggers; exactly equal does not.
func MatchAlert(holdings []Holding, today time.Time, hist []DayRecord) (*Alert, []AlertTrace) {
	var price, corp, hl []Alert
	var tr []AlertTrace
	for _, h := range holdings {
		if h.Symbol == "" || h.Manual || h.Last <= 0 || h.Prev <= 0 {
			continue
		}
		th, ok := defaultThreshold[h.Class]
		if h.AlertThresholdPct != nil {
			th, ok = *h.AlertThresholdPct, true
		}
		fx := h.FX // 0 when no FX rate exists for the currency: the position impact is then unknown (0)
		chg := (h.Last - h.Prev) / h.Prev * 100
		dir := "up"
		if chg < 0 {
			dir = "down"
		}
		pos := int64(math.Round(h.Units * (h.Last - h.Prev) * fx))
		t := AlertTrace{Asset: h.Symbol, ChangePct: round2(chg), Threshold: th}
		if !ok {
			t.Note = "no alert threshold for class " + h.Class
		} else if math.Abs(chg) > th {
			t.Triggered = true
			price = append(price, Alert{Type: AlertPrice, Asset: h.Symbol, Name: h.Name, ChangePct: round2(chg), Direction: dir, PositionIDR: pos})
		}
		tr = append(tr, t)
		if h.ExDate != nil && !h.ExDate.After(today) && today.Sub(*h.ExDate) <= 72*time.Hour {
			corp = append(corp, Alert{Type: AlertCorp, Asset: h.Symbol, Name: h.Name, Detail: "dividend", Amount: h.DividendPerShare, Direction: "up", ChangePct: round2(chg)})
		}
		if h.Hi52Prev > 0 && h.Last > h.Hi52Prev {
			hl = append(hl, Alert{Type: Alert52w, Asset: h.Symbol, Name: h.Name, Detail: "high", ChangePct: round2(chg), Direction: "up", PositionIDR: pos})
		} else if h.Lo52Prev > 0 && h.Last < h.Lo52Prev {
			hl = append(hl, Alert{Type: Alert52w, Asset: h.Symbol, Name: h.Name, Detail: "low", ChangePct: round2(chg), Direction: "down", PositionIDR: pos})
		}
	}
	byMove := func(a []Alert) {
		sort.SliceStable(a, func(i, j int) bool {
			if math.Abs(a[i].ChangePct) != math.Abs(a[j].ChangePct) {
				return math.Abs(a[i].ChangePct) > math.Abs(a[j].ChangePct)
			}
			return a[i].Asset < a[j].Asset
		})
	}
	byMove(price)
	byMove(corp)
	byMove(hl)
	var pick *Alert
	for _, l := range [][]Alert{price, corp, hl} { // price move first, then corporate action, then 52-week
		if len(l) > 0 {
			a := l[0]
			pick = &a
			break
		}
	}
	if pick != nil {
		pick.Key = fmt.Sprintf("%s:%s:%s", pick.Type, strings.ToUpper(pick.Asset), pick.Direction)
		pick.Repeated = len(hist) >= 2 && hist[0].AlertKey == pick.Key && hist[1].AlertKey == pick.Key
	}
	return pick, tr
}

// QuietMarket is true when no alert fired today nor in the five previous recorded days.
func QuietMarket(hist []DayRecord, alertToday bool) bool {
	if alertToday || len(hist) < 5 {
		return false
	}
	for _, h := range hist[:5] {
		if h.AlertKey != "" {
			return false
		}
	}
	return true
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

package domain

import (
	"math"
	"math/rand"
	"testing"
	"time"
)

func d(s string) time.Time { t, _ := time.Parse("2006-01-02", s); return t }

func series(vals ...float64) []Point {
	out := make([]Point, len(vals))
	for i, v := range vals {
		out[i] = Point{Date: d("2026-10-01").AddDate(0, 0, i), Value: v}
	}
	return out
}

var fx = FX{USDIDR: []Point{{d("2026-09-30"), 15000}, {d("2026-10-03"), 16000}}}

func TestValue(t *testing.T) {
	acq := d("2026-01-01")
	cases := []struct {
		name     string
		h        Holding
		in       Instrument
		divs     []Dividend
		mv, cost int64
		ret      float64
		avail    bool
		div      int64
		change1d float64
	}{
		{"idr stock", Holding{ID: "1", Symbol: "BBCA.JK", Units: 100, AvgCost: 9000, AcquiredAt: acq},
			Instrument{Class: "equity", Currency: "IDR", Series: series(10000, 10100)}, []Dividend{{d("2026-03-01"), 100, "IDR"}, {d("2025-12-01"), 500, "IDR"}},
			1010000, 900000, 12.22, true, 10000, 1},
		{"usd stock uses latest fx", Holding{ID: "2", Symbol: "AAPL", Units: 2, AvgCost: 3_000_000, AcquiredAt: acq},
			Instrument{Class: "equity", Currency: "USD", Series: series(200, 210)}, []Dividend{{d("2026-05-01"), 0.25, "USD"}},
			6_720_000, 6_000_000, 12, true, 8000, 5},
		{"other currency degrades to cost", Holding{ID: "3", Symbol: "X.L", Units: 5, AvgCost: 1000, AcquiredAt: acq},
			Instrument{Class: "equity", Currency: "GBp", Series: series(50, 60)}, []Dividend{{d("2026-05-01"), 1, "GBp"}},
			5000, 5000, 0, false, 0, 20},
		{"manual", Holding{ID: "4", Units: 12, AvgCost: 1_000_000, AcquiredAt: acq, Manual: &Manual{Currency: "IDR"}},
			Instrument{Class: "bond_sbn", Currency: "IDR", ManualPrice: 1_002_000, ManualDate: d("2026-10-10")}, nil,
			12_024_000, 12_000_000, 0.2, true, 0, 0},
		{"zero cost", Holding{ID: "5", Symbol: "Z", Units: 1, AvgCost: 0, AcquiredAt: acq},
			Instrument{Class: "equity", Currency: "IDR", Series: series(10)}, nil, 10, 0, 0, true, 0, 0},
		{"no prices at all", Holding{ID: "6", Symbol: "N", Units: 1, AvgCost: 7, AcquiredAt: acq},
			Instrument{Class: "equity", Currency: "IDR"}, nil, 7, 7, 0, false, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := Value(c.h, c.in, fx, c.divs)
			if v.MarketValue != c.mv || v.CostBasis != c.cost || v.TotalReturnPct != c.ret || v.ValuationAvailable != c.avail || v.DividendsIDR != c.div || v.Change1dPct != c.change1d {
				t.Fatalf("got mv=%d cost=%d ret=%v avail=%v div=%d 1d=%v", v.MarketValue, v.CostBasis, v.TotalReturnPct, v.ValuationAvailable, v.DividendsIDR, v.Change1dPct)
			}
			if v.TotalReturnIDR != c.mv-c.cost {
				t.Fatalf("return idr %d", v.TotalReturnIDR)
			}
			if len(v.Trend1y) == 0 && c.avail {
				t.Fatal("empty trend")
			}
		})
	}
}

func TestWeightsSumToHundredAndEmpty(t *testing.T) {
	ApplyWeights(nil) // must not panic
	zero := []HoldingView{{}, {}}
	ApplyWeights(zero)
	if zero[0].WeightPct != 0 {
		t.Fatal("empty portfolio must have zero weights")
	}
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 500; i++ {
		n := 1 + rng.Intn(15)
		vs := make([]HoldingView, n)
		for j := range vs {
			vs[j].Value = rng.Float64() * 1e9
		}
		ApplyWeights(vs)
		var sum float64
		for _, v := range vs {
			sum += v.WeightPct
		}
		if math.Abs(sum-100) > 0.005*float64(n)+1e-9 {
			t.Fatalf("weights sum %v for %d holdings", sum, n)
		}
	}
}

func TestFXRate(t *testing.T) {
	cases := []struct {
		ccy  string
		at   time.Time
		want float64
		ok   bool
	}{{"IDR", time.Time{}, 1, true}, {"USD", time.Time{}, 16000, true}, {"USD", d("2026-10-01"), 15000, true},
		{"USD", d("2026-10-05"), 16000, true}, {"USD", d("2020-01-01"), 15000, true}, {"EUR", time.Time{}, 0, false}, {"GBp", time.Time{}, 0, false}}
	for _, c := range cases {
		if got, ok := fx.Rate(c.ccy, c.at); got != c.want || ok != c.ok {
			t.Errorf("%s %v: %v %v", c.ccy, c.at, got, ok)
		}
	}
	if _, ok := (FX{}).Rate("USD", time.Time{}); ok {
		t.Fatal("no series must not be ok")
	}
}

func TestHistory(t *testing.T) {
	stock := Held{H: Holding{Symbol: "A", Units: 10, AvgCost: 100, AcquiredAt: d("2026-10-02")}, In: Instrument{Currency: "USD", Series: series(1, 2, 3, 4)}}
	manual := Held{H: Holding{Units: 2, AvgCost: 50, AcquiredAt: d("2026-09-01"), Manual: &Manual{}}, In: Instrument{Currency: "IDR", ManualPrice: 60}}
	noFX := Held{H: Holding{Symbol: "B", Units: 1, AvgCost: 500, AcquiredAt: d("2026-09-01")}, In: Instrument{Currency: "GBp", Series: series(9, 9, 9, 9)}}
	h := History([]Held{stock, manual, noFX}, fx, d("2026-10-01"), d("2026-10-04"))
	if len(h) != 4 {
		t.Fatalf("points %d", len(h))
	}
	// day 1: stock not yet acquired; manual 120 + noFX at cost 500
	if h[0].Value != 620 || h[0].Cost != 600 {
		t.Fatalf("day1 %+v", h[0])
	}
	// day 2 (10-02): stock 10 * 2 * fx(15000 until 10-03)
	if h[1].Value != 10*2*15000+120+500 || h[1].Cost != 1000+100+500 {
		t.Fatalf("day2 %+v", h[1])
	}
	// day 3 (10-03): fx 16000
	if h[2].Value != 10*3*16000+620 {
		t.Fatalf("day3 %+v", h[2])
	}
	if History(nil, fx, d("2026-10-01"), d("2026-10-04")) != nil {
		t.Fatal("empty portfolio has no history")
	}
	weekly := History([]Held{manual}, fx, d("2026-09-01"), d("2026-10-01"))
	if len(weekly) < 4 || weekly[len(weekly)-1].Date != d("2026-10-01") || weekly[0].Value != 120 {
		t.Fatalf("manual-only grid %+v", weekly)
	}
}

func TestDownsample(t *testing.T) {
	s := make([]int, 100)
	for i := range s {
		s[i] = i
	}
	o := Downsample(s, 30)
	if len(o) != 30 || o[0] != 0 || o[29] != 99 {
		t.Fatalf("%v", o)
	}
	if len(Downsample(s[:10], 30)) != 10 {
		t.Fatal("short input kept")
	}
}

func TestSumTotals(t *testing.T) {
	if tt := SumTotals(nil); tt.Count != 0 || tt.MarketValue != 0 || tt.ReturnPct != 0 {
		t.Fatalf("%+v", tt)
	}
	tt := SumTotals([]HoldingView{{Value: 110, Cost: 100, Divs: 3, ValuationAvailable: true}, {Value: 50, Cost: 50}})
	if tt.MarketValue != 160 || tt.UnrealizedPnl != 10 || tt.DividendsIDR != 3 || tt.UnvaluedCount != 1 || tt.ReturnPct != 6.67 {
		t.Fatalf("%+v", tt)
	}
}

func TestNewHoldingValidation(t *testing.T) {
	now := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC) // 10:00 WIB on the 17th
	ok := CreateInput{Symbol: "AAPL", Units: 1, AvgCost: 0, AcquiredAt: "2026-10-17"}
	if _, err := NewHolding("id", "u", ok, now); err != nil {
		t.Fatalf("today and zero cost are valid: %v", err)
	}
	man := &ManualInput{Name: "SBN", AssetClass: "bond_sbn", Currency: "IDR", CurrentPrice: 1}
	bad := []CreateInput{
		{Symbol: "A", Units: 0, AvgCost: 1, AcquiredAt: "2026-01-01"},
		{Symbol: "A", Units: -1, AvgCost: 1, AcquiredAt: "2026-01-01"},
		{Symbol: "A", Units: 1, AvgCost: -1, AcquiredAt: "2026-01-01"},
		{Symbol: "A", Units: 1, AvgCost: 1, AcquiredAt: "2026-10-18"},
		{Symbol: "A", Units: 1, AvgCost: 1, AcquiredAt: "17-10-2026"},
		{Symbol: "A", Units: 1, AvgCost: 1},
		{Symbol: "A", Units: 1e13, AvgCost: 1, AcquiredAt: "2026-01-01"},
		{Units: 1, AvgCost: 1, AcquiredAt: "2026-01-01"},
		{Symbol: "A", Manual: man, Units: 1, AvgCost: 1, AcquiredAt: "2026-01-01"},
		{Manual: &ManualInput{Name: "", AssetClass: "bond_sbn", Currency: "IDR", CurrentPrice: 1}, Units: 1, AvgCost: 1, AcquiredAt: "2026-01-01"},
		{Manual: &ManualInput{Name: "x", AssetClass: "equity", Currency: "IDR", CurrentPrice: 1}, Units: 1, AvgCost: 1, AcquiredAt: "2026-01-01"},
		{Manual: &ManualInput{Name: "x", AssetClass: "other", Currency: "USD", CurrentPrice: 1}, Units: 1, AvgCost: 1, AcquiredAt: "2026-01-01"},
		{Manual: &ManualInput{Name: "x", AssetClass: "other", Currency: "IDR", CurrentPrice: 0}, Units: 1, AvgCost: 1, AcquiredAt: "2026-01-01"},
	}
	for i, in := range bad {
		if _, err := NewHolding("id", "u", in, now); err == nil {
			t.Errorf("case %d should fail: %+v", i, in)
		}
	}
}

func TestApply(t *testing.T) {
	now := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	cat := Holding{ID: "1", Symbol: "A", Units: 1, AvgCost: 1, AcquiredAt: d("2026-01-01")}
	man := Holding{ID: "2", Units: 1, AvgCost: 1, AcquiredAt: d("2026-01-01"), Manual: &Manual{Price: 5}}
	f := func(v float64) *float64 { return &v }
	s := func(v string) *string { return &v }
	if _, err := cat.Apply(UpdateInput{}, now); err == nil {
		t.Error("empty patch")
	}
	if _, err := cat.Apply(UpdateInput{CurrentPrice: f(3)}, now); err == nil {
		t.Error("catalog holdings have no editable price")
	}
	if _, err := man.Apply(UpdateInput{CurrentPrice: f(0)}, now); err == nil {
		t.Error("zero price")
	}
	if _, err := cat.Apply(UpdateInput{AcquiredAt: s("2027-01-01")}, now); err == nil {
		t.Error("future date")
	}
	if _, err := cat.Apply(UpdateInput{Units: f(0)}, now); err == nil {
		t.Error("zero units")
	}
	got, err := man.Apply(UpdateInput{Units: f(2), CurrentPrice: f(9)}, now)
	if err != nil || got.Units != 2 || got.Manual.Price != 9 || man.Manual.Price != 5 || got.AvgCost != 1 {
		t.Fatalf("%+v %v (receiver must stay untouched)", got, err)
	}
}

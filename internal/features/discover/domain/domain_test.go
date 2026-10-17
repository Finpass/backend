package domain

import (
	"math"
	"testing"
)

func f(v float64) *float64 { return &v }

func eq(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return math.Abs(*a-*b) < 1e-9
}

func TestReturnPct(t *testing.T) {
	for _, c := range []struct {
		name       string
		last, base *float64
		want       *float64
	}{
		{"gain", f(110), f(100), f(10)},
		{"loss", f(75), f(100), f(-25)},
		{"short history: no base close, never a shortened period", f(110), nil, nil},
		{"no last price", nil, f(100), nil},
		{"zero base", f(1), f(0), nil},
		{"rounded to 4 decimals", f(100), f(300), f(-66.6667)},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := ReturnPct(c.last, c.base); !eq(got, c.want) {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestChooseValuation(t *testing.T) {
	for _, c := range []struct {
		name string
		in   *Fundamentals
		want *Valuation
	}{
		{"no fundamentals", nil, nil},
		{"PE first", &Fundamentals{TrailingPE: f(11.07), PriceToBook: f(2.4)}, &Valuation{"PE", 11.07}},
		{"negative PE falls to PB", &Fundamentals{TrailingPE: f(-3), PriceToBook: f(0.79)}, &Valuation{"PB", 0.79}},
		{"missing PE falls to PB", &Fundamentals{PriceToBook: f(1.27)}, &Valuation{"PB", 1.27}},
		{"zero PB falls to PS", &Fundamentals{PriceToBook: f(0), MarketCap: f(1000), TotalRevenue: f(500)}, &Valuation{"PS", 2}},
		{"PS needs both", &Fundamentals{MarketCap: f(1000)}, nil},
		{"PS needs positive revenue", &Fundamentals{MarketCap: f(1000), TotalRevenue: f(-5)}, nil},
		{"NaN ignored", &Fundamentals{TrailingPE: f(math.NaN()), PriceToBook: f(2)}, &Valuation{"PB", 2}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := ChooseValuation(c.in)
			if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
				t.Fatalf("got %+v want %+v", got, c.want)
			}
		})
	}
}

func TestChooseGrowth(t *testing.T) {
	per := func(d string, ni, rev *float64) Period { return Period{Date: d, NetIncome: ni, Revenue: rev} }
	for _, c := range []struct {
		name string
		in   *Fundamentals
		want *Growth
	}{
		{"none", nil, nil},
		{"earnings latest vs previous, any order", &Fundamentals{Periods: []Period{
			per("2024-12-31", f(100), f(1)), per("2025-12-31", f(110), f(1)), per("2023-12-31", f(50), f(1))}}, &Growth{"E", 10}},
		{"previous loss falls back to revenue", &Fundamentals{Periods: []Period{
			per("2024-12-31", f(-100), f(200)), per("2025-12-31", f(50), f(300))}}, &Growth{"R", 50}},
		{"single period", &Fundamentals{Periods: []Period{per("2025-12-31", f(50), f(300))}}, nil},
		{"skips periods without value", &Fundamentals{Periods: []Period{
			per("2022-12-31", f(10), nil), per("2023-12-31", nil, nil), per("2025-12-31", f(20), nil)}}, &Growth{"E", 100}},
		{"loss both years, revenue falling", &Fundamentals{Periods: []Period{
			per("2024-12-31", f(-5), f(400)), per("2025-12-31", f(-4), f(300))}}, &Growth{"R", -25}},
		{"nothing usable", &Fundamentals{Periods: []Period{per("2025-12-31", nil, nil), per("2024-12-31", nil, nil)}}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := ChooseGrowth(c.in)
			if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
				t.Fatalf("got %+v want %+v", got, c.want)
			}
		})
	}
}

func TestDivYield(t *testing.T) {
	for _, c := range []struct {
		name      string
		fund      *Fundamentals
		last, ttm *float64
		want      *float64
	}{
		{"fundamentals value is already a percent", &Fundamentals{DividendYieldPct: f(6.27)}, f(5225), f(1), f(6.27)},
		{"ttm over price", nil, f(200), f(5), f(2.5)},
		{"fundamentals zero falls to ttm", &Fundamentals{DividendYieldPct: f(0)}, f(100), f(4), f(4)},
		{"no data", nil, f(100), nil, nil},
		{"no price", nil, nil, f(4), nil},
		{"implausible yield dropped", nil, f(1.36), f(453.6), nil},
		{"boundary 100 kept", nil, f(10), f(10), f(100)},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := DivYield(c.fund, c.last, c.ttm); !eq(got, c.want) {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestIndustry(t *testing.T) {
	for _, c := range []struct {
		fund          *Fundamentals
		sector, class string
		want          string
	}{
		{&Fundamentals{Industry: "Banks - Regional"}, "financials", "equity", "Banks - Regional"},
		{nil, "consumer_non_cyclicals", "equity", "Consumer Non Cyclicals"},
		{nil, "unknown", "crypto", "Cryptocurrency"},
		{nil, "", "gold", "Gold"},
		{nil, "unknown", "etf", "ETF"},
		{nil, "unknown", "fx", "Currency"},
		{nil, "unknown", "weird", "weird"},
	} {
		if got := Industry(c.fund, c.sector, c.class); got != c.want {
			t.Errorf("%v: got %q want %q", c, got, c.want)
		}
	}
}

func TestDeriveShortHistoryAndMarketCap(t *testing.T) {
	m := Derive(Inputs{Symbol: "AAPL", Name: "Apple", AssetClass: "equity", Currency: "USD", LastPrice: f(342), LastTradeDate: "2026-10-15",
		Base7D: f(340), Base1Y: nil, TTMDividends: f(1.06)})
	if m.Ret1Y != nil || m.Ret7D == nil || m.MarketCap != nil || m.Valuation != nil || m.Growth != nil || m.DivYieldPct == nil {
		t.Fatalf("%+v", m)
	}
	if m.ClassGroup != "equity" || m.Industry != "Equity" {
		t.Fatalf("%+v", m)
	}
	g := Derive(Inputs{Symbol: "GC=F", AssetClass: "gold"})
	if g.ClassGroup != "commodity" {
		t.Fatalf("gold group %q", g.ClassGroup)
	}
}

func TestClassLabels(t *testing.T) {
	if id, en := ClassLabels("gold"); id != "Emas" || en != "Gold" {
		t.Fatal(id, en)
	}
	if id, en := GroupLabels("commodity"); id != "Komoditas" || en != "Commodities" {
		t.Fatal(id, en)
	}
}

func TestCursorRoundTripAndRejects(t *testing.T) {
	for _, c := range []Cursor{
		{Sort: "marketCap", Dir: "desc", Value: 6.41848167104512e14, Symbol: "BBCA.JK"},
		{Sort: "ret7d", Dir: "asc", Value: nil, Symbol: "AAPL"},
		{Sort: "name", Dir: "asc", Value: "apple inc", Symbol: "AAPL"},
		{Sort: "lastPrice", Dir: "desc", Value: 95.80999755859375, Symbol: "CL=F"},
	} {
		got, ok := DecodeCursor(c.Encode())
		if !ok || got != c {
			t.Fatalf("round trip %+v -> %+v ok=%v", c, got, ok)
		}
	}
	for _, bad := range []string{"", "%%%", "e30", "bm90LWpzb24"} {
		if _, ok := DecodeCursor(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
	for _, c := range []Cursor{
		{Sort: "nope", Dir: "asc", Symbol: "X"},
		{Sort: "ret7d", Dir: "up", Symbol: "X"},
		{Sort: "ret7d", Dir: "asc", Value: "text", Symbol: "X"},
		{Sort: "name", Dir: "asc", Value: 1.0, Symbol: "X"},
		{Sort: "ret7d", Dir: "asc", Symbol: ""},
	} {
		if _, ok := DecodeCursor(c.Encode()); ok {
			t.Errorf("accepted %+v", c)
		}
	}
}

// Sort stability: walking a list page by page with CursorFor and the keyset rule visits every row exactly once,
// including NULLs and ties, in both directions. The comparator mirrors ORDER BY col dir NULLS LAST, symbol ASC.
func TestKeysetWalkVisitsEachRowOnce(t *testing.T) {
	rows := []Metrics{
		{Symbol: "A", Ret7D: f(1)}, {Symbol: "B", Ret7D: f(1)}, {Symbol: "C", Ret7D: nil}, {Symbol: "D", Ret7D: f(-2)},
		{Symbol: "E", Ret7D: nil}, {Symbol: "F", Ret7D: f(1)}, {Symbol: "G", Ret7D: f(5)},
	}
	for _, dir := range []string{"asc", "desc"} {
		after := func(m Metrics, c Cursor) bool { // m sorts strictly after cursor position
			cv, mv := c.Value, m.Ret7D
			switch {
			case cv == nil:
				return mv == nil && m.Symbol > c.Symbol
			case mv == nil:
				return true
			}
			v := cv.(float64)
			if *mv == v {
				return m.Symbol > c.Symbol
			}
			if dir == "asc" {
				return *mv > v
			}
			return *mv < v
		}
		var seen []string
		var cur *Cursor
		for guard := 0; guard < 20; guard++ {
			var best *Metrics
			for i := range rows {
				m := rows[i]
				if cur != nil && !after(m, *cur) {
					continue
				}
				if best == nil || less(m, *best, dir) {
					best = &rows[i]
				}
			}
			if best == nil {
				break
			}
			seen = append(seen, best.Symbol)
			c := CursorFor(*best, "ret7d", dir)
			cur = &c
		}
		if len(seen) != len(rows) {
			t.Fatalf("%s: saw %v", dir, seen)
		}
		uniq := map[string]bool{}
		for _, s := range seen {
			uniq[s] = true
		}
		if len(uniq) != len(rows) {
			t.Fatalf("%s: duplicates %v", dir, seen)
		}
	}
}

func less(a, b Metrics, dir string) bool {
	switch {
	case a.Ret7D == nil && b.Ret7D == nil:
		return a.Symbol < b.Symbol
	case a.Ret7D == nil:
		return false
	case b.Ret7D == nil:
		return true
	case *a.Ret7D == *b.Ret7D:
		return a.Symbol < b.Symbol
	case dir == "asc":
		return *a.Ret7D < *b.Ret7D
	}
	return *a.Ret7D > *b.Ret7D
}

func TestCursorForNameLowercases(t *testing.T) {
	c := CursorFor(Metrics{Symbol: "X", Name: "Apple Inc"}, "name", "asc")
	if c.Value != "apple inc" {
		t.Fatal(c.Value)
	}
}

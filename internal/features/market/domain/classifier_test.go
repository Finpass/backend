package domain

import (
	"math"
	"math/rand"
	"reflect"
	"testing"
)

func f(v float64) *float64 { return &v }

// The YAML sample_assets as expected output. Inputs are the facts that would be known for each
// asset (market cap, vol, value); the YAML's classes follow from them through the thresholds.
func TestYAMLSampleAssets(t *testing.T) {
	type want struct{ class, sector, sharia, geo, vol, liq string }
	cases := []struct {
		in Input
		w  want
	}{
		{Input{Symbol: "BBCA.JK", SourceClass: "equity", Exchange: "JKT", MarketCapIDR: f(6e14), AnnualizedVol: f(.18), VolObs: 250, AvgDailyValueIDR: f(5e11)}, want{"equity_large_cap", "financials", "non_sharia", "indonesia", "medium", "high"}},
		{Input{Symbol: "BBRI.JK", SourceClass: "equity", Exchange: "JKT", MarketCapIDR: f(5e14), AnnualizedVol: f(.2), VolObs: 250, AvgDailyValueIDR: f(5e11)}, want{"equity_large_cap", "financials", "non_sharia", "indonesia", "medium", "high"}},
		{Input{Symbol: "TLKM.JK", SourceClass: "equity", Exchange: "JKT", MarketCapIDR: f(3e14), AnnualizedVol: f(.08), VolObs: 250, AvgDailyValueIDR: f(2e11)}, want{"equity_large_cap", "telecommunications", "sharia_compliant", "indonesia", "low", "high"}},
		{Input{Symbol: "ASII.JK", SourceClass: "equity", Exchange: "JKT", MarketCapIDR: f(2e14), AnnualizedVol: f(.2), VolObs: 250, AvgDailyValueIDR: f(1e11)}, want{"equity_large_cap", "industrials", "sharia_compliant", "indonesia", "medium", "high"}},
		{Input{Symbol: "UNVR.JK", SourceClass: "equity", Exchange: "JKT", MarketCapIDR: f(1e14), AnnualizedVol: f(.09), VolObs: 250, AvgDailyValueIDR: f(5e10)}, want{"equity_large_cap", "consumer_non_cyclicals", "sharia_compliant", "indonesia", "low", "high"}},
		{Input{Symbol: "ICBP.JK", SourceClass: "equity", Exchange: "JKT", MarketCapIDR: f(1e14), AnnualizedVol: f(.09), VolObs: 250, AvgDailyValueIDR: f(2e10)}, want{"equity_large_cap", "consumer_non_cyclicals", "sharia_compliant", "indonesia", "low", "high"}},
		{Input{Symbol: "ANTM.JK", SourceClass: "equity", Exchange: "JKT", MarketCapIDR: f(9e12), AnnualizedVol: f(.4), VolObs: 250, AvgDailyValueIDR: f(5e9)}, want{"equity_mid_small", "basic_materials", "sharia_compliant", "indonesia", "high", "medium"}},
		{Input{Symbol: "MAPI.JK", SourceClass: "equity", Exchange: "JKT", MarketCapIDR: f(5e12), AnnualizedVol: f(.3), VolObs: 250, AvgDailyValueIDR: f(3e9)}, want{"equity_mid_small", "consumer_cyclicals", "non_sharia", "indonesia", "high", "medium"}},
		{Input{Symbol: "GOTO.JK", SourceClass: "equity", Exchange: "JKT", MarketCapIDR: f(2e13), AnnualizedVol: f(.6), VolObs: 250, AvgDailyValueIDR: f(5e9)}, want{"equity_large_cap", "technology", "non_sharia", "indonesia", "high", "medium"}},
		{Input{Symbol: "AAPL", SourceClass: "equity", Exchange: "US", AnnualizedVol: f(.22), VolObs: 250, AvgDailyValueIDR: f(1e13)}, want{"equity_global", "technology", "non_sharia", "developed", "medium", "high"}},
		{Input{Symbol: "ETF_IDX30", SourceClass: "etf", Exchange: "JKT", AnnualizedVol: f(.15), VolObs: 250, AvgDailyValueIDR: f(2e10)}, want{"etf", "unknown", "sharia_compliant", "indonesia", "medium", "high"}},
		{Input{Symbol: "VOO", SourceClass: "etf", Exchange: "US", AnnualizedVol: f(.15), VolObs: 250, AvgDailyValueIDR: f(1e13)}, want{"etf", "unknown", "non_sharia", "developed", "medium", "high"}},
		{Input{Symbol: "RD_PU_001", SourceClass: "mutual_fund", FundKind: "money_market"}, want{"mutual_fund", "unknown", "mixed", "indonesia", "low", "high"}},
		{Input{Symbol: "RD_SAHAM_001", SourceClass: "mutual_fund", FundKind: "equity", AnnualizedVol: f(.15), VolObs: 250}, want{"mutual_fund", "unknown", "sharia_compliant", "indonesia", "medium", "medium"}},
		{Input{Symbol: "ORI024", SourceClass: "sbn", AvgDailyValueIDR: f(5e10)}, want{"bond_government", "unknown", "non_sharia", "indonesia", "low", "high"}},
		{Input{Symbol: "SR017", SourceClass: "sukuk", AvgDailyValueIDR: f(5e10)}, want{"sukuk", "unknown", "sharia_compliant", "indonesia", "low", "high"}},
		{Input{Symbol: "DEP_BCA_1M", SourceClass: "deposit"}, want{"deposits", "unknown", "non_sharia", "indonesia", "low", "high"}},
		{Input{Symbol: "GOLD_ANTAM", SourceClass: "gold", Currency: "IDR", AnnualizedVol: f(.15), VolObs: 250}, want{"gold", "unknown", "sharia_compliant", "indonesia", "medium", "high"}},
		{Input{Symbol: "BTC", SourceClass: "crypto", AnnualizedVol: f(.5), VolObs: 250}, want{"crypto", "unknown", "non_sharia", "global", "high", "high"}},
		{Input{Symbol: "ETH", SourceClass: "crypto", AnnualizedVol: f(.6), VolObs: 250}, want{"crypto", "unknown", "non_sharia", "global", "high", "high"}},
	}
	for _, c := range cases {
		r := Classify(c.in)
		got := want{r.AssetClass, r.Sector, r.Sharia, r.Geography, r.Volatility, r.Liquidity}
		if got != c.w {
			t.Errorf("%s: got %+v want %+v", c.in.Symbol, got, c.w)
		}
		if len(r.Trace) != 6 || r.Version != ClassifierVersion {
			t.Errorf("%s: trace %d version %q", c.in.Symbol, len(r.Trace), r.Version)
		}
	}
}

func TestMarketCapBoundary(t *testing.T) {
	for _, c := range []struct {
		cap  float64
		want string
	}{{LargeCapIDR + 1, "equity_large_cap"}, {LargeCapIDR, "equity_mid_small"}, {LargeCapIDR - 1, "equity_mid_small"}} {
		if g := Classify(Input{Symbol: "X.JK", SourceClass: "equity", Exchange: "JKT", MarketCapIDR: f(c.cap)}).AssetClass; g != c.want {
			t.Errorf("cap %v: %s want %s", c.cap, g, c.want)
		}
	}
	r := Classify(Input{Symbol: "X.JK", SourceClass: "equity", Exchange: "JKT"})
	if r.AssetClass != "equity_mid_small" || !r.Trace[0].Assumed {
		t.Errorf("missing cap must be an assumed mid/small: %+v", r.Trace[0])
	}
}

func TestVolatilityBoundaries(t *testing.T) {
	for _, c := range []struct {
		v    float64
		want string
	}{{0.0999, "low"}, {0.10, "medium"}, {0.25, "medium"}, {0.2501, "high"}, {0, "low"}} {
		if g := Classify(Input{Symbol: "Z", SourceClass: "etf", AnnualizedVol: f(c.v), VolObs: 100}).Volatility; g != c.want {
			t.Errorf("vol %v: %s want %s", c.v, g, c.want)
		}
	}
	if g := Classify(Input{Symbol: "Z", SourceClass: "etf", AnnualizedVol: f(.3), VolObs: MinVolObs - 1}).Volatility; g != Unknown {
		t.Errorf("too few observations: %s", g)
	}
	if g := Classify(Input{Symbol: "Z", SourceClass: "etf"}).Volatility; g != Unknown {
		t.Errorf("no prices: %s", g)
	}
}

func TestLiquidityBoundaries(t *testing.T) {
	for _, c := range []struct {
		v    float64
		want string
	}{{LiqHighAboveIDR + 1, "high"}, {LiqHighAboveIDR, "medium"}, {LiqMediumAboveID + 1, "medium"}, {LiqMediumAboveID, "low"}, {0, "low"}} {
		if g := Classify(Input{Symbol: "Z", SourceClass: "etf", AvgDailyValueIDR: f(c.v)}).Liquidity; g != c.want {
			t.Errorf("value %v: %s want %s", c.v, g, c.want)
		}
	}
	r := Classify(Input{Symbol: "Z", SourceClass: "etf", LiquidityNote: "no FX rate for CNY"})
	if r.Liquidity != Unknown || r.Trace[5].Rule != "no FX rate for CNY" {
		t.Errorf("unknown liquidity must carry the reason: %+v", r.Trace[5])
	}
}

func TestOverridesAndSpecials(t *testing.T) {
	if r := Classify(Input{Symbol: "GC=F", SourceClass: "gold", Currency: "USD"}); r.Liquidity != "high" || r.AssetClass != "gold" || r.Geography != "global" {
		t.Errorf("gold future: %+v", r)
	}
	if r := Classify(Input{Symbol: "IDR=X", SourceClass: "fx"}); r.AssetClass != Unknown || r.AlertThresholdPc != nil {
		t.Errorf("fx pair must be unknown: %+v", r)
	}
	if r := Classify(Input{Symbol: "5126.KL", Name: "Sentral REIT", SourceClass: "equity", Exchange: "KLS"}); r.AssetClass != "property" || r.Geography != "emerging" {
		t.Errorf("reit: %+v", r)
	}
	if r := Classify(Input{Symbol: "SPY", SourceClass: "etf", Exchange: "US"}); r.Geography != "developed" {
		t.Errorf("spy: %+v", r)
	}
	if r := Classify(Input{Symbol: "000001.SZ", SourceClass: "equity", Exchange: "SHZ"}); r.AssetClass != "equity_global" || r.Geography != "emerging" {
		t.Errorf("cn: %+v", r)
	}
	if r := Classify(Input{Symbol: "BBCA.JK", SourceClass: "equity", Exchange: "JKT", FundSector: "Financial Services"}); r.Sector != "financials" {
		t.Errorf("curated sector: %+v", r)
	}
	if r := Classify(Input{Symbol: "ZZZZ.JK", SourceClass: "equity", Exchange: "JKT", FundSector: "Energy"}); r.Sector != "energy" {
		t.Errorf("yfinance sector map: %+v", r)
	}
	if r := Classify(Input{Symbol: "ZZZZ.JK", SourceClass: "equity", Exchange: "JKT", FundSector: "Industrials", FundIndustry: "Marine Shipping"}); r.Sector != "transportation" {
		t.Errorf("industry map: %+v", r)
	}
	for _, c := range []struct {
		cls string
		pct float64
	}{{"crypto", 5}, {"gold", 2}, {"commodity", 5}, {"equity", 3}} {
		if r := Classify(Input{Symbol: "Q", SourceClass: c.cls, Exchange: "US"}); r.AlertThresholdPc == nil || *r.AlertThresholdPc != c.pct {
			t.Errorf("alert %s: %v", c.cls, r.AlertThresholdPc)
		}
	}
}

var (
	classSet  = set("equity_large_cap", "equity_mid_small", "equity_global", "etf", "mutual_fund", "bond_government", "bond_corporate", "sukuk", "deposits", "gold", "crypto", "property", "commodity", Unknown)
	sectorSet = set("financials", "consumer_non_cyclicals", "consumer_cyclicals", "energy", "basic_materials", "industrials", "healthcare", "technology", "telecommunications", "utilities", "property_real_estate", "infrastructure", "transportation", Unknown)
	shariaSet = set("sharia_compliant", "non_sharia", "mixed", Unknown)
	geoSet    = set("indonesia", "global", "emerging", "developed", Unknown)
	levelSet  = set("low", "medium", "high", Unknown)
)

func set(v ...string) map[string]bool {
	m := map[string]bool{}
	for _, s := range v {
		m[s] = true
	}
	return m
}

// Property: for any input every dimension holds a valid label or the explicit unknown, the trace has
// one step per dimension, and the function is deterministic.
func TestPropertyAllSixLabels(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	classes := []string{"equity", "etf", "gold", "commodity", "crypto", "fx", "mutual_fund", "sbn", "bond", "sukuk", "deposit", "property", "", "weird"}
	exch := []string{"", "JKT", "US", "LSE", "HKG", "SHZ", "SHH", "NSI", "SET", "GER", "TOR", "KLS", "PAR", "SES", "XXX"}
	sectors := []string{"", "Energy", "Financial Services", "Nonsense"}
	maybe := func(max float64) *float64 {
		if rng.Intn(3) == 0 {
			return nil
		}
		v := rng.Float64() * max
		return &v
	}
	for i := 0; i < 20000; i++ {
		in := Input{Symbol: string(rune('A'+rng.Intn(26))) + string(rune('A'+rng.Intn(26))), SourceClass: classes[rng.Intn(len(classes))], Exchange: exch[rng.Intn(len(exch))],
			FundSector: sectors[rng.Intn(len(sectors))], MarketCapIDR: maybe(1e15), AnnualizedVol: maybe(1), VolObs: rng.Intn(300), AvgDailyValueIDR: maybe(1e12)}
		r := Classify(in)
		if !classSet[r.AssetClass] || !sectorSet[r.Sector] || !shariaSet[r.Sharia] || !geoSet[r.Geography] || !levelSet[r.Volatility] || !levelSet[r.Liquidity] {
			t.Fatalf("invalid label for %+v: %+v", in, r)
		}
		if len(r.Trace) != 6 {
			t.Fatalf("trace len %d", len(r.Trace))
		}
		if r2 := Classify(in); !reflect.DeepEqual(r, r2) {
			t.Fatalf("not deterministic")
		}
	}
}

func TestStats(t *testing.T) {
	flat := make([]float64, 100)
	for i := range flat {
		flat[i] = 10
	}
	if v, n := AnnualizedVol(flat); v != 0 || n != 99 {
		t.Errorf("flat series: %v %d", v, n)
	}
	// alternating +1% / -1% daily: std ~ 1% -> ~15.87% annualized
	alt := []float64{100}
	for i := 0; i < 252; i++ {
		if i%2 == 0 {
			alt = append(alt, alt[i]*1.01)
		} else {
			alt = append(alt, alt[i]/1.01)
		}
	}
	if v, n := AnnualizedVol(alt); n != 252 || math.Abs(v-0.01*math.Sqrt(252)) > 0.002 {
		t.Errorf("alternating: %v %d", v, n)
	}
	if _, n := AnnualizedVol([]float64{1}); n != 0 {
		t.Errorf("single price")
	}
	if v, ok := AvgDailyValue([]float64{10, 20}, []float64{100, 300}); !ok || v != 200*15 {
		t.Errorf("avg value %v %v", v, ok)
	}
	nan := math.NaN()
	if _, ok := AvgDailyValue([]float64{10, 20}, []float64{nan, nan}); ok {
		t.Errorf("all-NaN volumes must not be ok")
	}
	fx := map[string]float64{"USD": 16000, "GBP": 21000}
	if x, ok := IDRFactor("GBp", fx); !ok || x != 210 {
		t.Errorf("GBp %v", x)
	}
	if _, ok := IDRFactor("CNY", fx); ok {
		t.Errorf("CNY has no rate")
	}
	if x, ok := IDRFactor("IDR", fx); !ok || x != 1 {
		t.Errorf("IDR")
	}
}

func TestNewsCategory(t *testing.T) {
	for _, c := range []struct{ src, ch, reg, title, want string }{
		{"CNBC Indonesia", "market", "ID", "", CatCapitalMarkets},
		{"CNN Indonesia", "ekonomi", "ID", "", CatMacro},
		{"CNN Indonesia", "internasional", "ID", "", CatGlobal},
		{"Bisnis.com", "Energi & Tambang", "ID", "", CatRealSector},
		{"Finnhub/Yahoo", "company", "", "", CatCapitalMarkets},
		{"Finnhub/Reuters", "business", "", "", CatGlobal},
		{"Kontan", "all", "ID", "IHSG ditutup naik", CatCapitalMarkets},
		{"Kontan", "all", "ID", "Inflasi September turun", CatMacro},
		{"Kontan", "all", "ID", "Pabrik baru dibuka", CatRealSector},
		{"Kontan", "all", "ID", "Cuaca hari ini", CatMacro},
		{"Finnhub/X", "unheard", "", "Nothing", CatGlobal},
		{"Bisnis.com", "Kabar Jabar", "ID", "", CatMacro},
	} {
		if g := NewsCategory(c.src, c.ch, c.reg, c.title); g != c.want || !ValidNewsCategory(g) {
			t.Errorf("%v: got %s", c, g)
		}
	}
}

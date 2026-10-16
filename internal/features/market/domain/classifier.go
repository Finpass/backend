package domain

import (
	"regexp"
	"strings"
)

// ClassifierVersion: YAML ASSET CLASSIFIER 1.0 plus the implementation choices listed in README "Market data".
const ClassifierVersion = "1.0.0"

const Unknown = "unknown"

// Thresholds from the YAML.
const (
	LargeCapIDR      = 10_000_000_000_000.0 // market cap > Rp 10 triliun
	VolLowBelow      = 0.10                 // < 10%
	VolHighAbove     = 0.25                 // > 25%
	LiqHighAboveIDR  = 10_000_000_000.0     // daily value > Rp 10 miliar
	LiqMediumAboveID = 1_000_000_000.0      // > Rp 1 miliar
)

// Input is everything the classifier may look at. Pointers are nil when the data is not available.
type Input struct {
	Symbol      string
	Name        string
	SourceClass string // equity|etf|gold|commodity|crypto|fx|mutual_fund|sbn|bond|sukuk|deposit|property
	Exchange    string
	Country     string
	Currency    string
	FundKind    string // mutual_fund only: money_market|equity|mixed|fixed_income

	FundSector       string   // yfinance sector from fundamentals
	FundIndustry     string   // yfinance industry
	MarketCapIDR     *float64 // fundamentals.market_cap converted to IDR
	AnnualizedVol    *float64 // AnnualizedVol(); nil when no prices
	VolObs           int
	AvgDailyValueIDR *float64 // AvgDailyValue() x IDR factor; nil when no volume or no FX
	LiquidityNote    string   // why AvgDailyValueIDR is nil (e.g. no FX rate), copied into the trace
}

type TraceStep struct {
	Dimension string `json:"dimension"`
	Value     string `json:"value"`
	Rule      string `json:"rule"`
	Assumed   bool   `json:"assumed,omitempty"` // true when a default stood in for missing data
}

type Result struct {
	Symbol           string
	AssetClass       string
	Sector           string
	Sharia           string
	Geography        string
	Volatility       string
	Liquidity        string
	AlertThresholdPc *float64
	Trace            []TraceStep
	Version          string
}

var alertThreshold = map[string]float64{
	"equity_large_cap": 3, "equity_mid_small": 5, "equity_global": 3, "etf": 3, "mutual_fund": 2,
	"bond_government": 0.5, "bond_corporate": 0.5, "sukuk": 0.5, "gold": 2, "crypto": 5, "property": 3, "commodity": 5,
}

// Curated holds the manual curation we can source from the YAML itself (sample_assets). Keys are
// upper-case symbols without the .JK suffix. Everything else is unknown rather than guessed.
type Curated struct {
	Sector, Sharia, Geography string
}

var curated = map[string]Curated{
	"BBCA": {"financials", "non_sharia", ""}, "BBRI": {"financials", "non_sharia", ""},
	"TLKM": {"telecommunications", "sharia_compliant", ""}, "ASII": {"industrials", "sharia_compliant", ""},
	"UNVR": {"consumer_non_cyclicals", "sharia_compliant", ""}, "ICBP": {"consumer_non_cyclicals", "sharia_compliant", ""},
	"ANTM": {"basic_materials", "sharia_compliant", ""}, "MAPI": {"consumer_cyclicals", "non_sharia", ""},
	"GOTO": {"technology", "non_sharia", ""}, "AAPL": {"technology", "non_sharia", "developed"},
	"ETF_IDX30": {"", "sharia_compliant", ""}, "VOO": {"", "non_sharia", ""},
	"RD_PU_001": {"", "mixed", ""}, "RD_SAHAM_001": {"", "sharia_compliant", ""},
	"ORI024": {"", "non_sharia", ""}, "SR017": {"", "sharia_compliant", ""}, "DEP_BCA_1M": {"", "non_sharia", ""},
	"GOLD_ANTAM": {"", "sharia_compliant", "indonesia"}, "BTC": {"", "non_sharia", ""}, "ETH": {"", "non_sharia", ""},
	"BITCOIN": {"", "non_sharia", ""}, "ETHEREUM": {"", "non_sharia", ""},
}

func curatedFor(symbol string) (Curated, bool) {
	k := strings.ToUpper(strings.TrimSuffix(strings.ToUpper(symbol), ".JK"))
	c, ok := curated[k]
	return c, ok
}

var yfSector = map[string]string{
	"Financial Services": "financials", "Consumer Defensive": "consumer_non_cyclicals", "Consumer Cyclical": "consumer_cyclicals",
	"Energy": "energy", "Basic Materials": "basic_materials", "Industrials": "industrials", "Healthcare": "healthcare",
	"Technology": "technology", "Communication Services": "telecommunications", "Utilities": "utilities", "Real Estate": "property_real_estate",
}

var (
	transportRe = regexp.MustCompile(`(?i)airline|marine shipping|trucking|railroad|airport|logistics|integrated freight`)
	infraRe     = regexp.MustCompile(`(?i)infrastructure|engineering & construction`)
	reitRe      = regexp.MustCompile(`(?i)\breit\b|real estate investment trust|\bDIRE\b`)
)

var (
	developedEx = map[string]bool{"US": true, "LSE": true, "GER": true, "PAR": true, "TOR": true, "SES": true, "HKG": true, "JPX": true}
	emergingEx  = map[string]bool{"SHH": true, "SHZ": true, "NSI": true, "SET": true, "KLS": true, "KSC": true}
)

func has(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Classify is pure and deterministic: same Input, same Result (and trace order).
func Classify(in Input) Result {
	r := Result{Symbol: in.Symbol, Version: ClassifierVersion}
	step := func(dim, val, rule string, assumed bool) {
		r.Trace = append(r.Trace, TraceStep{Dimension: dim, Value: val, Rule: rule, Assumed: assumed})
	}
	cur, hasCur := curatedFor(in.Symbol)

	// 1. asset class
	idx := in.Exchange == "JKT" || in.Country == "ID"
	switch in.SourceClass {
	case "equity":
		switch {
		case reitRe.MatchString(in.Name):
			r.AssetClass = "property"
			step("asset_class", r.AssetClass, "equity whose name matches REIT/DIRE -> property", false)
		case idx && in.MarketCapIDR != nil && *in.MarketCapIDR > LargeCapIDR:
			r.AssetClass = "equity_large_cap"
			step("asset_class", r.AssetClass, "IDX equity, market cap > Rp 10 triliun", false)
		case idx && in.MarketCapIDR != nil:
			r.AssetClass = "equity_mid_small"
			step("asset_class", r.AssetClass, "IDX equity, market cap <= Rp 10 triliun", false)
		case idx:
			r.AssetClass = "equity_mid_small"
			step("asset_class", r.AssetClass, "IDX equity without fundamentals (source lists only the 100 largest, min cap > Rp 13 triliun): assumed mid/small", true)
		default:
			r.AssetClass = "equity_global"
			step("asset_class", r.AssetClass, "equity listed outside Indonesia", false)
		}
	case "etf":
		r.AssetClass = "etf"
		step("asset_class", r.AssetClass, "source class etf", false)
	case "gold", "commodity", "crypto", "property":
		r.AssetClass = in.SourceClass
		step("asset_class", r.AssetClass, "source class "+in.SourceClass, false)
	case "mutual_fund":
		r.AssetClass = "mutual_fund"
		step("asset_class", r.AssetClass, "source class mutual_fund", false)
	case "sbn":
		r.AssetClass = "bond_government"
		step("asset_class", r.AssetClass, "SBN/ORI/SUN", false)
	case "bond":
		r.AssetClass = "bond_corporate"
		step("asset_class", r.AssetClass, "source class bond", false)
	case "sukuk":
		r.AssetClass = "sukuk"
		step("asset_class", r.AssetClass, "source class sukuk", false)
	case "deposit":
		r.AssetClass = "deposits"
		step("asset_class", r.AssetClass, "source class deposit", false)
	default:
		r.AssetClass = Unknown
		step("asset_class", Unknown, "source class "+in.SourceClass+" is not an investable class in the YAML (e.g. fx pair)", false)
	}
	if t, ok := alertThreshold[r.AssetClass]; ok {
		r.AlertThresholdPc = &t
	}
	isEq := strings.HasPrefix(r.AssetClass, "equity_")

	// 2. sector (applies to equity_large_cap, equity_mid_small, etf)
	switch {
	case hasCur && cur.Sector != "":
		r.Sector = cur.Sector
		step("sector", r.Sector, "manual curation from YAML sample_assets", false)
	case r.AssetClass == "equity_large_cap" || r.AssetClass == "equity_mid_small" || r.AssetClass == "etf":
		r.Sector = Unknown
		switch {
		case transportRe.MatchString(in.FundIndustry):
			r.Sector = "transportation"
		case infraRe.MatchString(in.FundIndustry):
			r.Sector = "infrastructure"
		case yfSector[in.FundSector] != "":
			r.Sector = yfSector[in.FundSector]
		}
		if r.Sector == Unknown {
			step("sector", Unknown, "no IDX-IC data and no yfinance sector for this symbol", false)
		} else {
			step("sector", r.Sector, "yfinance sector/industry "+in.FundSector+" / "+in.FundIndustry+" mapped to IDX-IC", false)
		}
	default:
		r.Sector = Unknown
		step("sector", Unknown, "not applicable: dimension applies to equity_large_cap, equity_mid_small, etf", false)
	}

	// 3. sharia status
	appliesSharia := isEq || has([]string{"etf", "mutual_fund", "bond_government", "bond_corporate", "sukuk", "deposits", "gold"}, r.AssetClass)
	switch {
	case hasCur && cur.Sharia != "":
		r.Sharia = cur.Sharia
		step("sharia_status", r.Sharia, "manual curation from YAML sample_assets", false)
	case r.AssetClass == "sukuk":
		r.Sharia = "sharia_compliant"
		step("sharia_status", r.Sharia, "sukuk = sharia bond", false)
	case !appliesSharia:
		r.Sharia = Unknown
		step("sharia_status", Unknown, "not applicable to this asset class", false)
	default:
		r.Sharia = Unknown
		step("sharia_status", Unknown, "no ISSI / DSN-MUI / OJK list in the data (gap: needs manual curation)", false)
	}

	// 4. geography
	switch {
	case hasCur && cur.Geography != "":
		r.Geography = cur.Geography
		step("geography", r.Geography, "manual curation from YAML sample_assets", false)
	case idx:
		r.Geography = "indonesia"
		step("geography", r.Geography, "listed in Indonesia (exchange JKT / country ID)", false)
	case has([]string{"mutual_fund", "bond_government", "bond_corporate", "sukuk", "deposits"}, r.AssetClass):
		r.Geography = "indonesia"
		step("geography", r.Geography, "Indonesian product class (OJK/BI/LPS universe in the YAML)", true)
	case r.AssetClass == "crypto" || r.AssetClass == "commodity" || r.AssetClass == "gold":
		r.Geography = "global"
		step("geography", r.Geography, "not tied to one bourse", false)
	case developedEx[in.Exchange]:
		r.Geography = "developed"
		step("geography", r.Geography, "exchange "+in.Exchange+" is a developed market", false)
	case emergingEx[in.Exchange]:
		r.Geography = "emerging"
		step("geography", r.Geography, "exchange "+in.Exchange+" is an emerging market", false)
	default:
		r.Geography = Unknown
		step("geography", Unknown, "exchange unknown", false)
	}

	// 5. volatility
	switch {
	case r.AssetClass == Unknown:
		r.Volatility = Unknown
		step("volatility_class", Unknown, "asset class unknown: not an investable instrument", false)
	case in.AnnualizedVol != nil && in.VolObs >= MinVolObs:
		v := *in.AnnualizedVol
		switch {
		case v < VolLowBelow:
			r.Volatility = "low"
		case v > VolHighAbove:
			r.Volatility = "high"
		default:
			r.Volatility = "medium"
		}
		step("volatility_class", r.Volatility, "annualized vol from prices (<10% low, 10-25% medium, >25% high)", false)
	case in.AnnualizedVol != nil:
		r.Volatility = Unknown
		step("volatility_class", Unknown, "fewer than 30 daily returns", false)
	case has([]string{"deposits", "bond_government", "sukuk"}, r.AssetClass) || (r.AssetClass == "mutual_fund" && in.FundKind == "money_market"):
		r.Volatility = "low"
		step("volatility_class", "low", "no price series; YAML lists this class as low volatility", true)
	default:
		r.Volatility = Unknown
		step("volatility_class", Unknown, "no price history", false)
	}

	// 6. liquidity: rule-based overrides first, then daily value
	sym := strings.ToUpper(in.Symbol)
	switch {
	case r.AssetClass == Unknown:
		r.Liquidity = Unknown
		step("liquidity_class", Unknown, "asset class unknown: not an investable instrument", false)
	case r.AssetClass == "deposits":
		r.Liquidity = "high"
		step("liquidity_class", "high", "override: deposito", false)
	case r.AssetClass == "mutual_fund" && in.FundKind == "money_market":
		r.Liquidity = "high"
		step("liquidity_class", "high", "override: RD pasar uang", false)
	case r.AssetClass == "mutual_fund" && in.FundKind == "equity":
		r.Liquidity = "medium"
		step("liquidity_class", "medium", "override: RD saham", false)
	case r.AssetClass == "gold":
		r.Liquidity = "high"
		step("liquidity_class", "high", "override: emas", false)
	case r.AssetClass == "crypto" && (sym == "BTC" || sym == "ETH" || sym == "BITCOIN" || sym == "ETHEREUM"):
		r.Liquidity = "high"
		step("liquidity_class", "high", "override: BTC/ETH", false)
	case in.AvgDailyValueIDR != nil:
		v := *in.AvgDailyValueIDR
		switch {
		case v > LiqHighAboveIDR:
			r.Liquidity = "high"
		case v > LiqMediumAboveID:
			r.Liquidity = "medium"
		default:
			r.Liquidity = "low"
		}
		step("liquidity_class", r.Liquidity, "avg volume x avg price in IDR (>Rp 10 miliar high, >Rp 1 miliar medium, else low)", false)
	default:
		r.Liquidity = Unknown
		note := in.LiquidityNote
		if note == "" {
			note = "no volume data"
		}
		step("liquidity_class", Unknown, note, false)
	}
	return r
}

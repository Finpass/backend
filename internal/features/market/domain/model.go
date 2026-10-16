package domain

import (
	"encoding/json"
	"errors"
	"time"
)

var ErrNotFound = errors.New("market: not found")

// Asset is a catalog row. Nullable text columns are returned as "" and nullable numbers as nil.
type Asset struct {
	Symbol      string `json:"symbol"`
	Name        string `json:"name"`
	AssetClass  string `json:"assetClass"` // source class: equity|etf|gold|commodity|crypto|fx
	Currency    string `json:"currency"`
	Exchange    string `json:"exchange"`
	Country     string `json:"country"`
	HasMetadata bool   `json:"hasMetadata"`
}

// AssetHit is a search result: asset + latest close + classified class (empty if not classified).
type AssetHit struct {
	Asset
	Classified    string     `json:"classified"` // Asset Classifier class, e.g. equity_large_cap
	LastPrice     *float64   `json:"lastPrice"`
	LastTradeDate *time.Time `json:"lastTradeDate"`
}

type Price struct {
	Symbol string
	Date   time.Time
	Open   *float64
	High   *float64
	Low    *float64
	Close  float64
	Volume *float64
}

type Dividend struct {
	Symbol   string
	ExDate   time.Time
	Amount   float64
	Currency string
}

type YieldPoint struct {
	TradeDate   time.Time
	Tenor       string
	TenorMonths *float64
	YieldPct    float64
}

type Fundamentals struct {
	Symbol     string
	Sector     string
	Industry   string
	MarketCap  *float64
	Info       json.RawMessage
	Statements json.RawMessage
}

type Classification struct {
	Symbol           string
	AssetClass       string
	Sector           string
	ShariaStatus     string
	Geography        string
	Volatility       string
	Liquidity        string
	AnnualizedVol    *float64
	AvgDailyValueIDR *float64
	MarketCapIDR     *float64
	AlertThresholdPc *float64
	Trace            []TraceStep
	Version          string
	ClassifiedAt     time.Time
}

type Article struct {
	ID          string    `json:"id"`
	Source      string    `json:"source"`
	Channel     string    `json:"channel"`
	Category    string    `json:"category"`
	Title       string    `json:"title"`
	Summary     string    `json:"summary"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"publishedAt"`
	Language    string    `json:"language"`
	Region      string    `json:"region"`
	Tickers     []string  `json:"tickers"`
	Sentiment   *float64  `json:"sentiment"`
}

// NewsFilter: Category ""/"all" = no filter; Q is a full-text query (websearch syntax);
// Tickers matches articles mentioning ANY of the symbols; Limit defaults to 20, max 100.
type NewsFilter struct {
	Category string
	Q        string
	Tickers  []string
	Limit    int
}

type NewsPage struct {
	Items      []Article
	NextCursor string // "" when there is no further page
}

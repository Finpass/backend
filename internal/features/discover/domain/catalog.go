package domain

import (
	"strings"

	"shenzen/backend/internal/shared/apperr"
)

// ClassInfo is the filter group and its labels. The raw assets.asset_class values are equity, etf, gold,
// commodity, crypto, fx; gold and commodity share the "commodity" filter group.
type ClassInfo struct{ Group, LabelID, LabelEN, Industry string }

var classes = map[string]ClassInfo{
	"equity":    {"equity", "Saham", "Stocks", "Equity"},
	"etf":       {"etf", "ETF", "ETFs", "ETF"},
	"crypto":    {"crypto", "Kripto", "Crypto", "Cryptocurrency"},
	"commodity": {"commodity", "Komoditas", "Commodities", "Commodity"},
	"gold":      {"commodity", "Emas", "Gold", "Gold"},
	"fx":        {"fx", "Valas", "Currency", "Currency"},
}

// ClassGroups are the accepted `class` filter values besides "all", in display order.
var ClassGroups = []string{"equity", "etf", "crypto", "commodity", "fx"}

var groupLabels = map[string][2]string{
	"equity": {"Saham", "Stocks"}, "etf": {"ETF", "ETFs"}, "crypto": {"Kripto", "Crypto"},
	"commodity": {"Komoditas", "Commodities"}, "fx": {"Valas", "Currency"},
}

func ClassGroup(assetClass string) string {
	if c, ok := classes[assetClass]; ok {
		return c.Group
	}
	return assetClass
}

// ClassLabels returns the Indonesian and English label of a raw asset class.
func ClassLabels(assetClass string) (id, en string) {
	if c, ok := classes[assetClass]; ok {
		return c.LabelID, c.LabelEN
	}
	return assetClass, assetClass
}

// GroupLabels returns the labels of a filter group.
func GroupLabels(group string) (id, en string) {
	if l, ok := groupLabels[group]; ok {
		return l[0], l[1]
	}
	return group, group
}

// Industry: fundamentals industry, else the classifier sector (humanised), else the readable asset class label.
func Industry(f *Fundamentals, classSector, assetClass string) string {
	if f != nil && strings.TrimSpace(f.Industry) != "" {
		return strings.TrimSpace(f.Industry)
	}
	if s := strings.TrimSpace(classSector); s != "" && s != "unknown" {
		return humanise(s)
	}
	if c, ok := classes[assetClass]; ok {
		return c.Industry
	}
	return assetClass
}

func humanise(s string) string {
	parts := strings.Split(strings.ReplaceAll(s, "_", " "), " ")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

// Sort keys of the API mapped to stored columns.
var sortColumns = map[string]string{
	"marketCap": "market_cap", "lastPrice": "last_price", "ret7d": "ret_7d", "ret1y": "ret_1y",
	"divYield": "div_yield_pct", "name": "name_lc",
}

// SortColumn returns the stored column of an API sort key.
func SortColumn(sort string) (string, bool) { c, ok := sortColumns[sort]; return c, ok }

var ErrAssetNotFound = apperr.New(apperr.NotFound, "Asset not found")

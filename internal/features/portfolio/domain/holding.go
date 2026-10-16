// Package domain holds the portfolio rules: what a holding is, how it is validated and how it is valued in IDR.
package domain

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"shenzen/backend/internal/shared/apperr"
)

const MaxHoldingsPerUser = 200

var (
	ErrNotFound      = apperr.New(apperr.NotFound, "Holding not found")
	ErrLimitReached  = apperr.New(apperr.Conflict, "Holding limit reached (200 per user)")
	ErrUnknownSymbol = errors.New("portfolio: unknown symbol")
)

// ManualClasses are the classes a user can give to an instrument that is not in the catalog.
var ManualClasses = []string{"bond_sbn", "mutual_fund", "deposit", "other"}

// Holding is the stored position. Units are in the instrument's own unit; AvgCost and manual prices are IDR per unit.
type Holding struct {
	ID         string
	UserID     string
	Symbol     string  // "" for a manual instrument
	Manual     *Manual // nil for a catalog instrument
	Units      float64
	AvgCost    float64
	AcquiredAt time.Time // date, UTC midnight
	CreatedAt  time.Time
}

type Manual struct {
	Name           string
	Class          string
	Currency       string
	Price          float64
	PriceUpdatedAt time.Time
}

func (h Holding) IsManual() bool { return h.Manual != nil }

type ManualInput struct {
	Name         string
	AssetClass   string
	Currency     string
	CurrentPrice float64
}

type CreateInput struct {
	Symbol     string
	Manual     *ManualInput
	Units      float64
	AvgCost    float64
	AcquiredAt string
}

// UpdateInput fields are pointers: nil means unchanged.
type UpdateInput struct {
	Units        *float64
	AvgCost      *float64
	AcquiredAt   *string
	CurrentPrice *float64
}

type Repository interface {
	// Insert stores h unless the user already has `limit` holdings (ErrLimitReached).
	Insert(ctx context.Context, h Holding, limit int) error
	List(ctx context.Context, userID string) ([]Holding, error)
	Get(ctx context.Context, userID, id string) (Holding, error) // ErrNotFound
	Update(ctx context.Context, h Holding) error                 // ErrNotFound when not owned
	Delete(ctx context.Context, userID, id string) error         // ErrNotFound when not owned
}

type AssetHit struct {
	Symbol        string
	Name          string
	AssetClass    string
	Currency      string
	Exchange      string
	LastPrice     float64
	LastTradeDate string
}

type AssetInfo struct {
	Symbol, Name, AssetClass, Currency, Exchange string
}

type Dividend struct {
	ExDate   time.Time
	Amount   float64
	Currency string
}

// Market is the read side of the market data, adapted in the composition root.
type Market interface {
	Search(ctx context.Context, q, class string, limit int) ([]AssetHit, error)
	Assets(ctx context.Context, symbols []string) (map[string]AssetInfo, error)
	// Series returns ascending daily closes in the asset's own currency since `from`; symbols without rows are absent.
	Series(ctx context.Context, symbols []string, from time.Time) (map[string][]Point, error)
	Dividends(ctx context.Context, symbols []string) (map[string][]Dividend, error)
	// USDIDR is the ascending IDR=X close series since `from`.
	USDIDR(ctx context.Context, from time.Time) ([]Point, error)
}

var wib = time.FixedZone("WIB", 7*3600)

// Today is the calendar date in Jakarta, as UTC midnight.
func Today(now time.Time) time.Time {
	y, m, d := now.In(wib).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func bad(field, msg string) apperr.Detail { return apperr.Detail{Field: field, Message: msg} }

func checkNumbers(units, avgCost *float64, acquiredAt string, today time.Time, out *[]apperr.Detail) (date time.Time) {
	if units != nil && (math.IsNaN(*units) || *units <= 0 || *units > 1e12) {
		*out = append(*out, bad("units", "must be greater than 0 and at most 1e12"))
	}
	if avgCost != nil && (math.IsNaN(*avgCost) || *avgCost < 0 || *avgCost > 1e12) {
		*out = append(*out, bad("avgCost", "must be between 0 and 1e12"))
	}
	if acquiredAt != "" {
		d, err := time.Parse("2006-01-02", acquiredAt)
		switch {
		case err != nil:
			*out = append(*out, bad("acquiredAt", "must be a date YYYY-MM-DD"))
		case d.After(today):
			*out = append(*out, bad("acquiredAt", "must not be in the future"))
		case d.Year() < 1990:
			*out = append(*out, bad("acquiredAt", "must be 1990 or later"))
		default:
			date = d
		}
	}
	return date
}

func checkManual(m ManualInput, out *[]apperr.Detail) {
	if n := len([]rune(strings.TrimSpace(m.Name))); n < 1 || n > 120 {
		*out = append(*out, bad("manual.name", "must be 1 to 120 characters"))
	}
	if !contains(ManualClasses, m.AssetClass) {
		*out = append(*out, bad("manual.assetClass", "must be one of bond_sbn, mutual_fund, deposit, other"))
	}
	if m.Currency != "IDR" {
		*out = append(*out, bad("manual.currency", "only IDR is supported"))
	}
	checkPrice("manual.currentPrice", m.CurrentPrice, out)
}

func checkPrice(field string, p float64, out *[]apperr.Detail) {
	if math.IsNaN(p) || p <= 0 || p > 1e12 {
		*out = append(*out, bad(field, "must be greater than 0 and at most 1e12"))
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// NewHolding validates a create request. Whether the symbol exists is checked by the application layer.
func NewHolding(id, userID string, in CreateInput, now time.Time) (Holding, error) {
	var d []apperr.Detail
	in.Symbol = strings.TrimSpace(in.Symbol)
	switch {
	case in.Symbol == "" && in.Manual == nil:
		d = append(d, bad("symbol", "symbol or manual is required"))
	case in.Symbol != "" && in.Manual != nil:
		d = append(d, bad("manual", "provide either symbol or manual, not both"))
	case in.Manual != nil:
		checkManual(*in.Manual, &d)
	}
	if in.AcquiredAt == "" {
		d = append(d, bad("acquiredAt", "required"))
	}
	date := checkNumbers(&in.Units, &in.AvgCost, in.AcquiredAt, Today(now), &d)
	if len(d) > 0 {
		return Holding{}, apperr.Validation(d...)
	}
	h := Holding{ID: id, UserID: userID, Symbol: in.Symbol, Units: in.Units, AvgCost: in.AvgCost, AcquiredAt: date, CreatedAt: now.UTC()}
	if in.Manual != nil {
		h.Manual = &Manual{Name: strings.TrimSpace(in.Manual.Name), Class: in.Manual.AssetClass, Currency: "IDR", Price: in.Manual.CurrentPrice, PriceUpdatedAt: now.UTC()}
	}
	return h, nil
}

// Apply validates a PATCH and returns the changed copy; the receiver is untouched.
func (h Holding) Apply(in UpdateInput, now time.Time) (Holding, error) {
	var d []apperr.Detail
	if in.Units == nil && in.AvgCost == nil && in.AcquiredAt == nil && in.CurrentPrice == nil {
		return h, apperr.Validation(bad("body", "at least one field is required"))
	}
	acq := ""
	if in.AcquiredAt != nil {
		acq = *in.AcquiredAt
		if acq == "" {
			d = append(d, bad("acquiredAt", "must be a date YYYY-MM-DD"))
		}
	}
	date := checkNumbers(in.Units, in.AvgCost, acq, Today(now), &d)
	if in.CurrentPrice != nil {
		if !h.IsManual() {
			d = append(d, bad("currentPrice", "only manual holdings have an editable price"))
		} else {
			checkPrice("currentPrice", *in.CurrentPrice, &d)
		}
	}
	if len(d) > 0 {
		return h, apperr.Validation(d...)
	}
	out := h
	if in.Units != nil {
		out.Units = *in.Units
	}
	if in.AvgCost != nil {
		out.AvgCost = *in.AvgCost
	}
	if !date.IsZero() {
		out.AcquiredAt = date
	}
	if in.CurrentPrice != nil {
		m := *h.Manual
		m.Price, m.PriceUpdatedAt = *in.CurrentPrice, now.UTC()
		out.Manual = &m
	}
	return out, nil
}

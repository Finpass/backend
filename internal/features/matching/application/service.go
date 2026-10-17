package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"shenzen/backend/internal/features/matching/domain"
)

type Deps struct {
	Profiles domain.ProfileReader
	Holdings domain.HoldingsReader
	Catalog  domain.Catalog
	News     domain.NewsReader
	Macro    domain.MacroReader
	History  domain.HistoryReader
	Lookup   domain.Lookuper // product comparison; nil disables it
	Audit    domain.Auditor  // suitability trail; nil disables it
	Now      func() time.Time
}

type Service struct{ d Deps }

func NewService(d Deps) *Service { return &Service{d} }

type ProfileSummary struct {
	Risk    string `json:"riskProfile"`
	Ladder  int    `json:"ladderLevel"`
	Sharia  string `json:"sharia"`
	Horizon string `json:"horizon"`
}

type ProductResult struct {
	Profile ProfileSummary `json:"profile"`
	domain.ProductOutput
	Summary     domain.Text `json:"summary"`
	GeneratedAt string      `json:"generatedAt"`
}

func (s *Service) Products(ctx context.Context, userID string) (ProductResult, error) {
	p, err := s.d.Profiles.Profile(ctx, userID)
	if err != nil {
		return ProductResult{}, err
	}
	holdings, err := s.d.Holdings.Holdings(ctx, userID)
	if err != nil {
		slog.WarnContext(ctx, "matching: holdings unavailable", "err", err)
		holdings = nil
	}
	var cands []domain.Candidate
	rf := 0.0
	var sig domain.MacroSignals
	if p.Ladder > 2 {
		if cands, err = s.d.Catalog.Candidates(ctx); err != nil {
			return ProductResult{}, err
		}
		if rf, err = s.d.Catalog.RiskFree(ctx); err != nil {
			rf = 0
		}
		if sig, err = s.d.Catalog.Signals(ctx); err != nil {
			sig = domain.MacroSignals{}
		}
	}
	out := domain.Match(p, cands, holdings, rf, sig)
	res := ProductResult{
		Profile:       ProfileSummary{p.Risk, p.Ladder, p.Sharia, fmt.Sprintf("%.1f", p.HorizonYears())},
		ProductOutput: out, GeneratedAt: s.d.Now().UTC().Format(time.RFC3339),
	}
	res.Summary = summaryText(p, out.Trace.TargetAllocation.Target)
	s.audit(ctx, userID, "portfolio", portfolioAudit(res))
	return res, nil
}

// Recommendation is what the dashboard home consumes (BACKEND-A adapts this to its own interface).
type Recommendation struct {
	TextID      string       `json:"textId"`
	TextEn      string       `json:"textEn"`
	Allocation  []AllocSlice `json:"allocation"`
	Source      string       `json:"source"`
	GeneratedAt time.Time    `json:"generatedAt"`
}

type AllocSlice struct {
	Label   string `json:"label"`
	LabelID string `json:"labelId"`
	Pct     int    `json:"pct"`
}

// Recommender is the interface other slices adapt: rules decide, text is a deterministic template.
type Recommender interface {
	Recommend(ctx context.Context, userID string) (Recommendation, error)
}

var _ Recommender = (*Service)(nil)

func (s *Service) Recommend(ctx context.Context, userID string) (Recommendation, error) {
	p, err := s.d.Profiles.Profile(ctx, userID)
	if err != nil {
		return Recommendation{}, err
	}
	t := domain.TargetAllocation(p).Target
	r := Recommendation{Source: "rules", GeneratedAt: s.d.Now().UTC(), Allocation: []AllocSlice{}}
	for _, w := range domain.SortedWeights(t) {
		l := domain.ClassLabels[w.Class]
		r.Allocation = append(r.Allocation, AllocSlice{Label: l.EN, LabelID: l.ID, Pct: w.Pct})
	}
	sum := summaryText(p, t)
	r.TextID, r.TextEn = sum.ID, sum.EN
	return r, nil
}

func summaryText(p domain.Profile, t map[string]float64) domain.Text {
	w := domain.SortedWeights(t)
	var id, en []string
	for i, x := range w {
		if i == 4 {
			break
		}
		l := domain.ClassLabels[x.Class]
		id = append(id, fmt.Sprintf("%s %d%%", l.ID, x.Pct))
		en = append(en, fmt.Sprintf("%s %d%%", l.EN, x.Pct))
	}
	if p.Ladder <= 2 {
		return domain.Text{
			ID: fmt.Sprintf("Anda berada di level %d tangga keuangan. Fokus dulu pada dana darurat dan utang; simpan dana di instrumen aman: %s.", p.Ladder, strings.Join(id, ", ")),
			EN: fmt.Sprintf("You are at financial ladder level %d. Focus on the emergency fund and debt first; keep funds in safe instruments: %s.", p.Ladder, strings.Join(en, ", ")),
		}
	}
	return domain.Text{
		ID: fmt.Sprintf("Untuk profil risiko %s dan level %d, alokasi target Anda: %s. Ini alokasi berbasis aturan, bukan jaminan hasil.", p.Risk, p.Ladder, strings.Join(id, ", ")),
		EN: fmt.Sprintf("For a %s risk profile at level %d, your target allocation is: %s. This is a rule-based allocation, not a guarantee of returns.", p.Risk, p.Ladder, strings.Join(en, ", ")),
	}
}

type DailyResult struct {
	Profile  domain.Profile
	Output   domain.DailyOutput
	Degraded []string // sources that failed: holdings, news, macro
}

// Daily runs the four content layers for the user's local time.
func (s *Service) Daily(ctx context.Context, userID string, localNow time.Time) (DailyResult, error) {
	p, err := s.d.Profiles.Profile(ctx, userID)
	if err != nil {
		return DailyResult{}, err
	}
	res := DailyResult{Profile: p}
	in := domain.DailyInput{Profile: p, Now: localNow}
	if in.Holdings, err = s.d.Holdings.Holdings(ctx, userID); err != nil {
		slog.WarnContext(ctx, "matching: holdings unavailable", "err", err)
		res.Degraded = append(res.Degraded, "holdings")
	}
	var tickers []string
	for _, h := range in.Holdings {
		if h.Symbol != "" {
			tickers = append(tickers, h.Symbol)
		}
	}
	if in.Articles, err = s.d.News.Recent(ctx, localNow.Add(-72*time.Hour), tickers, 80); err != nil {
		slog.WarnContext(ctx, "matching: news unavailable", "err", err)
		res.Degraded = append(res.Degraded, "news")
	}
	if in.Macro, err = s.d.Macro.Indicators(ctx, localNow.Add(-72*time.Hour)); err != nil {
		slog.WarnContext(ctx, "matching: macro unavailable", "err", err)
		res.Degraded = append(res.Degraded, "macro")
	}
	if in.History, err = s.d.History.History(ctx, userID, 7); err != nil {
		slog.WarnContext(ctx, "matching: history unavailable", "err", err)
	}
	res.Output = domain.MatchDaily(in)
	return res, nil
}

// audit is best effort: a failed write must never break the answer the user asked for.
func (s *Service) audit(ctx context.Context, userID, kind string, payload any) {
	if s.d.Audit == nil {
		return
	}
	if err := s.d.Audit.Record(ctx, userID, kind, payload); err != nil {
		slog.WarnContext(ctx, "matching: audit failed", "kind", kind, "err", err)
	}
}

func portfolioAudit(r ProductResult) map[string]any {
	picks := map[string][]string{}
	for _, c := range r.Portfolio {
		for _, a := range c.Assets {
			if a.Kind == "asset" {
				picks[c.Class] = append(picks[c.Class], a.Ticker)
			}
		}
	}
	return map[string]any{
		"profile": r.Profile, "investable": r.Investable, "target": r.Trace.TargetAllocation.Target,
		"modifiers": r.Trace.TargetAllocation.Modifiers, "gates": r.Trace.GatesApplied, "picks": picks,
	}
}

var ErrCompareCount = errors.New("matching: compare needs 2 to 4 distinct symbols")
var ErrCompareDisabled = errors.New("matching: compare unavailable")

type CompareResult struct {
	Profile ProfileSummary `json:"profile"`
	domain.CompareOutput
	NotFound    []string `json:"notFound"`
	GeneratedAt string   `json:"generatedAt"`
}

// Compare lines up 2 to 4 assets against the user's profile with the same gates and score as the portfolio.
func (s *Service) Compare(ctx context.Context, userID string, symbols []string) (CompareResult, error) {
	if s.d.Lookup == nil {
		return CompareResult{}, ErrCompareDisabled
	}
	seen, want := map[string]bool{}, []string{}
	for _, sym := range symbols {
		sym = strings.ToUpper(strings.TrimSpace(sym))
		if sym != "" && !seen[sym] {
			seen[sym] = true
			want = append(want, sym)
		}
	}
	if len(want) < domain.MinCompare || len(want) > domain.MaxCompare {
		return CompareResult{}, ErrCompareCount
	}
	p, err := s.d.Profiles.Profile(ctx, userID)
	if err != nil {
		return CompareResult{}, err
	}
	found, err := s.d.Lookup.Lookup(ctx, want)
	if err != nil {
		return CompareResult{}, err
	}
	by := map[string]domain.Candidate{}
	for _, c := range found {
		by[c.Symbol] = c
	}
	var cands []domain.Candidate
	res := CompareResult{NotFound: []string{}}
	for _, sym := range want { // keep the order the user picked
		if c, ok := by[sym]; ok {
			cands = append(cands, c)
		} else {
			res.NotFound = append(res.NotFound, sym)
		}
	}
	rf, err := s.d.Catalog.RiskFree(ctx)
	if err != nil {
		rf = 0
	}
	sig, err := s.d.Catalog.Signals(ctx)
	if err != nil {
		sig = domain.MacroSignals{}
	}
	res.Profile = ProfileSummary{p.Risk, p.Ladder, p.Sharia, fmt.Sprintf("%.1f", p.HorizonYears())}
	res.CompareOutput = domain.Compare(p, cands, rf, sig)
	res.GeneratedAt = s.d.Now().UTC().Format(time.RFC3339)
	if len(cands) >= domain.MinCompare {
		s.audit(ctx, userID, "comparison", comparisonAudit(res))
	}
	return res, nil
}

func comparisonAudit(r CompareResult) map[string]any {
	rows := make([]map[string]any, 0, len(r.Assets))
	for _, a := range r.Assets {
		rows = append(rows, map[string]any{"symbol": a.Symbol, "class": a.Class, "eligible": a.Gate.Eligible, "gate": a.Gate.Gate, "score": a.Score.Total, "rank": a.Rank})
	}
	return map[string]any{"profile": r.Profile, "assets": rows, "best": r.Best}
}

// Suitability returns the user's latest audit records, newest first.
func (s *Service) Suitability(ctx context.Context, userID string, limit int) ([]domain.SuitabilityRecord, error) {
	if s.d.Audit == nil {
		return []domain.SuitabilityRecord{}, nil
	}
	return s.d.Audit.List(ctx, userID, limit)
}

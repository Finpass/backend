package app

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	authhttp "shenzen/backend/internal/features/auth/transport"
	dashdomain "shenzen/backend/internal/features/dashboard/domain"
	matchapp "shenzen/backend/internal/features/matching/application"
	matchdomain "shenzen/backend/internal/features/matching/domain"
	matchinfra "shenzen/backend/internal/features/matching/infrastructure"
	matchhttp "shenzen/backend/internal/features/matching/transport"
	profileapp "shenzen/backend/internal/features/profile/application"
	profiledomain "shenzen/backend/internal/features/profile/domain"
	profileinfra "shenzen/backend/internal/features/profile/infrastructure"
	waapp "shenzen/backend/internal/features/whatsapp/application"
	wadomain "shenzen/backend/internal/features/whatsapp/domain"
	wainfra "shenzen/backend/internal/features/whatsapp/infrastructure"
	wahttp "shenzen/backend/internal/features/whatsapp/transport"
)

var (
	matchOnce sync.Once
	matchSvc  *matchapp.Service
)

// matching returns the process-wide matching service (its catalog cache is shared by every caller).
func matching(d Deps) *matchapp.Service {
	matchOnce.Do(func() {
		matchSvc = matchapp.NewService(matchapp.Deps{
			Profiles: profileBridge{profileinfra.NewRepo(d.Pool)},
			Holdings: matchinfra.NewHoldings(d.Pool),
			Catalog:  matchinfra.NewCatalog(d.Pool),
			News:     matchinfra.NewNews(d.Pool),
			Macro:    matchinfra.NewMacro(d.Pool),
			History:  matchinfra.NewHistory(d.Pool),
			Now:      time.Now,
		})
	})
	return matchSvc
}

// mountAdvice wires the matching engine (/advice) and the WhatsApp delivery (/whatsapp, /briefing).
func mountAdvice(r chi.Router, d Deps, authH *authhttp.Handler, profiles *profileapp.Service) {
	jakarta, _ := time.LoadLocation(wadomain.DefaultTimezone)
	ms := matching(d)
	matchhttp.New(ms, authH.RequireAuth, authhttp.UserID, jakarta, time.Now).Routes(r)

	c := d.Config.WA
	link := "http://localhost:3000"
	if len(d.Config.CORSOrigins) > 0 {
		link = strings.TrimRight(d.Config.CORSOrigins[0], "/")
	}
	repo := wainfra.NewRepo(d.Pool)
	svc := waapp.NewService(waapp.Deps{
		Store:   repo,
		Matcher: waMatcher{ms},
		Sender:  wainfra.NewGoWA(c.BaseURL, c.BasicUser, c.BasicPassword),
		Names:   nameBridge{profileinfra.NewRepo(d.Pool)},
		Link:    link, Secret: c.WebhookSecret, NewID: newID, Now: time.Now,
	})
	wahttp.New(svc, authH.RequireAuth, authhttp.UserID).Routes(r)
	profiles.AlsoErase(svc.Erase)

	if c.DailyEnabled {
		ctx := d.Ctx
		if ctx == nil {
			ctx = context.Background()
		}
		go svc.RunScheduler(ctx, 30*time.Second)
	} else {
		slog.Info("whatsapp daily scheduler disabled (set WA_DAILY_ENABLED=true to enable)")
	}
}

// ---- adapters: slices only know their own small interfaces ----

// profileBridge turns the latest profile assessment (result + answers) into the matching engine's Profile.
type profileBridge struct{ repo profileinfra.Repo }

func (b profileBridge) Profile(ctx context.Context, userID string) (matchdomain.Profile, error) {
	a, err := b.repo.LatestAssessment(ctx, userID)
	if errors.Is(err, profiledomain.ErrNotFound) {
		return matchdomain.Profile{}, matchdomain.ErrNoProfile
	}
	if err != nil {
		return matchdomain.Profile{}, err
	}
	name, _ := b.repo.FirstName(ctx, userID)
	return toMatchProfile(userID, firstWord(name), a), nil
}

func toMatchProfile(userID, name string, a profiledomain.Assessment) matchdomain.Profile {
	o, ans := a.Output, a.Input.Answers
	str := func(k string) string { s, _ := ans[k].(string); return s }
	num := func(k string) int64 { f, _ := ans[k].(float64); return int64(f) }
	deps := 0
	switch str("q5") {
	case "1", "2", "3":
		deps = int(str("q5")[0] - '0')
	case "4_plus":
		deps = 4
	}
	var held []string
	if l, ok := ans["q23"].([]any); ok {
		for _, v := range l {
			if s, ok := v.(string); ok {
				held = append(held, s)
			}
		}
	}
	h := o.FinancialHealth
	return matchdomain.Profile{
		UserID: userID, Name: name, Risk: o.FinalProfile, Ladder: o.LadderLevel,
		Health:          matchdomain.Health{Security: h.FinancialSecurity, Safety: h.FinancialSafety, Growth: h.FinancialGrowth, Wisdom: h.FinancialWisdom, Freedom: h.FinancialFreedom, Total: h.TotalScore, Category: h.Category},
		EmergencyMonths: o.EmergencyFundMonths, EmergencyTargetMax: o.EmergencyTargetMonths.Max,
		Income: o.Metrics.MonthlyIncome, Expenses: o.Metrics.MonthlyExpenses, Surplus: o.Metrics.MonthlySurplus, Installment: o.Metrics.MonthlyInstallment,
		Investable: num("q8"), Savings: num("q4"), Dependents: deps,
		Employment: str("q10"), Knowledge: str("q16"), Sharia: o.Gates.Sharia, Goal: str("q17"), GoalHorizon: str("q19"), InvestHorizon: str("q9"), GoalAmount: num("q18"),
		HeldKinds: held, Priorities: o.BriefingPriorities,
	}
}

func firstWord(s string) string {
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return ""
}

type nameBridge struct{ repo profileinfra.Repo }

func (n nameBridge) FirstName(ctx context.Context, userID string) (string, error) {
	s, err := n.repo.FirstName(ctx, userID)
	return firstWord(s), err
}

// waMatcher maps the matching engine's daily output to the WhatsApp templates' content and the selection keys stored per run.
type waMatcher struct{ m *matchapp.Service }

func (w waMatcher) Content(ctx context.Context, userID string, localNow time.Time) (wadomain.Content, map[string]string, error) {
	res, err := w.m.Daily(ctx, userID, localNow)
	if errors.Is(err, matchdomain.ErrNoProfile) {
		return wadomain.Content{}, nil, wadomain.ErrNoProfile
	}
	if err != nil {
		return wadomain.Content{}, nil, err
	}
	p, o := res.Profile, res.Output
	c := wadomain.Content{
		Name: p.Name, Risk: p.Risk, Ladder: p.Ladder, HealthTotal: p.Health.Total, HealthCat: p.Health.Category,
		Quiet: o.QuietMarket, Degraded: len(res.Degraded) > 0 && (contains(res.Degraded, "news") || contains(res.Degraded, "holdings")),
		Nudge: wadomain.Nudge{Key: o.Nudge.Key, Action: wadomain.Text{ID: o.Nudge.Action.ID, EN: o.Nudge.Action.EN}, Reason: wadomain.Text{ID: o.Nudge.Reason.ID, EN: o.Nudge.Reason.EN}},
	}
	keys := map[string]string{"nudgeKey": o.Nudge.Key}
	if a := o.Alert; a != nil {
		c.Alert = &wadomain.Alert{Type: a.Type, Asset: a.Asset, Direction: a.Direction, Detail: a.Detail, ChangePct: a.ChangePct, PositionIDR: a.PositionIDR, Amount: a.Amount, Repeated: a.Repeated}
		keys["alertKey"] = a.Key
	}
	toNews := func(n matchdomain.NewsPick) wadomain.News {
		return wadomain.News{Title: n.Title, Summary: n.Summary, URL: n.URL, Reason: wadomain.Text{ID: n.Reason.ID, EN: n.Reason.EN}, NumbersSafe: n.NumbersSafe, Followup: n.Followup}
	}
	if n := o.News; n != nil {
		x := toNews(*n)
		c.News = &x
		keys["newsId"], keys["newsTopic"] = n.ID, n.Topic
	}
	for _, n := range o.MoreNews {
		c.MoreNews = append(c.MoreNews, toNews(n))
	}
	if m := o.Macro; m != nil {
		c.Macro = &wadomain.Macro{Label: wadomain.Text{ID: m.Label.ID, EN: m.Label.EN}, ValueText: m.ValueText, Change: m.Change, Headline: m.Headline, Reason: wadomain.Text{ID: m.Reason.ID, EN: m.Reason.EN}}
	}
	return c, keys, nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// recommenderBridge gives the dashboard home its recommendation from the product matching rules.
type recommenderBridge struct{ m *matchapp.Service }

func (b recommenderBridge) Recommend(ctx context.Context, userID string, _ dashdomain.Profile, _ time.Time) (dashdomain.Recommendation, error) {
	r, err := b.m.Recommend(ctx, userID)
	if err != nil {
		return dashdomain.Recommendation{}, err // the dashboard falls back to its own rules table
	}
	out := dashdomain.Recommendation{TextID: r.TextID, TextEn: r.TextEn, Source: r.Source, GeneratedAt: r.GeneratedAt.Format(time.RFC3339), Allocation: []dashdomain.Allocation{}}
	for _, a := range r.Allocation {
		out.Allocation = append(out.Allocation, dashdomain.Allocation{Label: a.Label, Pct: float64(a.Pct)})
	}
	return out, nil
}

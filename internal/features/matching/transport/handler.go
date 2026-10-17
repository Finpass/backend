package transport

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"shenzen/backend/internal/features/matching/application"
	"shenzen/backend/internal/features/matching/domain"
	"shenzen/backend/internal/platform/httpx"
	"shenzen/backend/internal/shared/apperr"
)

type Handler struct {
	svc    *application.Service
	auth   func(http.Handler) http.Handler
	userID func(context.Context) string
	loc    *time.Location
	now    func() time.Time
}

func New(svc *application.Service, auth func(http.Handler) http.Handler, userID func(context.Context) string, loc *time.Location, now func() time.Time) *Handler {
	return &Handler{svc, auth, userID, loc, now}
}

func (h *Handler) Routes(r chi.Router) {
	r.Route("/advice", func(r chi.Router) {
		r.Use(h.auth)
		r.Method(http.MethodGet, "/products", httpx.HandlerFunc(h.products))
		r.Method(http.MethodGet, "/daily", httpx.HandlerFunc(h.daily))
		r.Method(http.MethodPost, "/compare", httpx.HandlerFunc(h.compare))
		r.Method(http.MethodGet, "/suitability", httpx.HandlerFunc(h.suitability))
	})
}

func mapErr(err error) error {
	if errors.Is(err, domain.ErrNoProfile) {
		return apperr.New(apperr.NotFound, "Complete the financial profile first")
	}
	if errors.Is(err, application.ErrCompareCount) {
		return apperr.New(apperr.BadRequest, "Pick 2 to 4 different products to compare")
	}
	if errors.Is(err, application.ErrCompareDisabled) {
		return apperr.New(apperr.Unprocessable, "Product comparison is unavailable")
	}
	return err
}

func (h *Handler) products(w http.ResponseWriter, r *http.Request) error {
	res, err := h.svc.Products(r.Context(), h.userID(r.Context()))
	if err != nil {
		return mapErr(err)
	}
	httpx.OK(w, http.StatusOK, "Product matching fetched", res)
	return nil
}

type dailyResponse struct {
	Date     string            `json:"date"`
	Degraded []string          `json:"degraded"`
	Alert    *domain.Alert     `json:"alert"`
	News     *domain.NewsPick  `json:"news"`
	MoreNews []domain.NewsPick `json:"moreNews"`
	Macro    *domain.MacroPick `json:"macro"`
	Nudge    domain.Nudge      `json:"nudge"`
	Quiet    bool              `json:"quietMarket"`
	Trace    domain.DailyTrace `json:"trace"`
}

func (h *Handler) daily(w http.ResponseWriter, r *http.Request) error {
	now := h.now().In(h.loc)
	res, err := h.svc.Daily(r.Context(), h.userID(r.Context()), now)
	if err != nil {
		return mapErr(err)
	}
	o := res.Output
	if res.Degraded == nil {
		res.Degraded = []string{}
	}
	httpx.OK(w, http.StatusOK, "Daily content matching fetched", dailyResponse{
		Date: now.Format("2006-01-02"), Degraded: res.Degraded, Alert: o.Alert, News: o.News, MoreNews: o.MoreNews, Macro: o.Macro, Nudge: o.Nudge, Quiet: o.QuietMarket, Trace: o.Trace,
	})
	return nil
}

type compareRequest struct {
	Symbols []string `json:"symbols"`
}

func (h *Handler) compare(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[compareRequest](w, r)
	if err != nil {
		return err
	}
	res, err := h.svc.Compare(r.Context(), h.userID(r.Context()), req.Symbols)
	if err != nil {
		return mapErr(err)
	}
	httpx.OK(w, http.StatusOK, "Product comparison fetched", res)
	return nil
}

func (h *Handler) suitability(w http.ResponseWriter, r *http.Request) error {
	items, err := h.svc.Suitability(r.Context(), h.userID(r.Context()), 20)
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Suitability records fetched", map[string]any{"items": items})
	return nil
}

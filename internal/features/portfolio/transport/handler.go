// Package transport exposes the portfolio use cases over HTTP under /assets and /portfolio.
package transport

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"shenzen/backend/internal/features/portfolio/application"
	"shenzen/backend/internal/features/portfolio/domain"
	"shenzen/backend/internal/platform/httpx"
)

type Handler struct {
	svc    *application.Service
	auth   func(http.Handler) http.Handler
	userID func(context.Context) string
}

// auth and userID come from the auth slice via the composition root.
func New(svc *application.Service, auth func(http.Handler) http.Handler, userID func(context.Context) string) *Handler {
	return &Handler{svc: svc, auth: auth, userID: userID}
}

func (h *Handler) Routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(h.auth)
		r.Method(http.MethodGet, "/assets/search", httpx.HandlerFunc(h.search))
		r.Route("/portfolio/holdings", func(r chi.Router) {
			r.Method(http.MethodGet, "/", httpx.HandlerFunc(h.list))
			r.Method(http.MethodPost, "/", httpx.HandlerFunc(h.create))
			r.Method(http.MethodPatch, "/{id}", httpx.HandlerFunc(h.update))
			r.Method(http.MethodDelete, "/{id}", httpx.HandlerFunc(h.remove))
		})
	})
}

type searchHit struct {
	Symbol        string  `json:"symbol"`
	Name          string  `json:"name"`
	AssetClass    string  `json:"assetClass"`
	Currency      string  `json:"currency"`
	Exchange      string  `json:"exchange"`
	LastPrice     float64 `json:"lastPrice"`
	LastTradeDate string  `json:"lastTradeDate"`
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	hits, err := h.svc.Search(r.Context(), q.Get("q"), q.Get("class"), limit)
	if err != nil {
		return err
	}
	out := make([]searchHit, len(hits))
	for i, a := range hits {
		out[i] = searchHit(a)
	}
	httpx.OK(w, http.StatusOK, "Assets found", out)
	return nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	l, err := h.svc.List(r.Context(), h.userID(r.Context()))
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Holdings fetched", l)
	return nil
}

type manualRequest struct {
	Name         string  `json:"name"`
	AssetClass   string  `json:"assetClass"`
	Currency     string  `json:"currency"`
	CurrentPrice float64 `json:"currentPrice"`
}

type createRequest struct {
	Symbol     string         `json:"symbol"`
	Manual     *manualRequest `json:"manual"`
	Units      *float64       `json:"units" validate:"required"`
	AvgCost    *float64       `json:"avgCost" validate:"required"`
	AcquiredAt string         `json:"acquiredAt" validate:"required"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[createRequest](w, r)
	if err != nil {
		return err
	}
	in := domain.CreateInput{Symbol: req.Symbol, Units: *req.Units, AvgCost: *req.AvgCost, AcquiredAt: req.AcquiredAt}
	if req.Manual != nil {
		m := domain.ManualInput(*req.Manual)
		in.Manual = &m
	}
	v, err := h.svc.Create(r.Context(), h.userID(r.Context()), in)
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusCreated, "Holding added", v)
	return nil
}

type updateRequest struct {
	Units        *float64 `json:"units"`
	AvgCost      *float64 `json:"avgCost"`
	AcquiredAt   *string  `json:"acquiredAt"`
	CurrentPrice *float64 `json:"currentPrice"`
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[updateRequest](w, r)
	if err != nil {
		return err
	}
	v, err := h.svc.Update(r.Context(), h.userID(r.Context()), chi.URLParam(r, "id"), domain.UpdateInput(req))
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Holding updated", v)
	return nil
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) error {
	if err := h.svc.Delete(r.Context(), h.userID(r.Context()), chi.URLParam(r, "id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

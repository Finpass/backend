// Package transport exposes /discover.
package transport

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"shenzen/backend/internal/features/discover/application"
	"shenzen/backend/internal/platform/httpx"
	"shenzen/backend/internal/shared/apperr"
)

type Handler struct {
	svc    *application.Service
	auth   func(http.Handler) http.Handler
	userID func(context.Context) string
}

func New(svc *application.Service, auth func(http.Handler) http.Handler, userID func(context.Context) string) *Handler {
	return &Handler{svc: svc, auth: auth, userID: userID}
}

func (h *Handler) Routes(r chi.Router) {
	r.Route("/discover", func(r chi.Router) {
		r.Use(h.auth)
		r.Method(http.MethodGet, "/assets", httpx.HandlerFunc(h.list))
		r.Method(http.MethodGet, "/facets", httpx.HandlerFunc(h.facets))
		r.Method(http.MethodPut, "/watchlist/{symbol}", httpx.HandlerFunc(h.star))
		r.Method(http.MethodDelete, "/watchlist/{symbol}", httpx.HandlerFunc(h.unstar))
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	v := r.URL.Query()
	p := application.Params{Q: v.Get("q"), Class: v.Get("class"), Exchange: v.Get("exchange"), Industry: v.Get("industry"),
		Sort: v.Get("sort"), Dir: v.Get("dir"), Cursor: v.Get("cursor")}
	if s := v.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return apperr.Validation(apperr.Detail{Field: "limit", Message: "must be a number"})
		}
		p.Limit = n
	}
	switch strings.ToLower(v.Get("watchlist")) {
	case "", "false":
	case "true":
		p.Watchlist = true
	default:
		return apperr.Validation(apperr.Detail{Field: "watchlist", Message: "must be true or false"})
	}
	out, err := h.svc.List(r.Context(), h.userID(r.Context()), p)
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Assets fetched", out)
	return nil
}

func (h *Handler) facets(w http.ResponseWriter, r *http.Request) error {
	out, err := h.svc.Facets(r.Context())
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Facets fetched", out)
	return nil
}

func (h *Handler) star(w http.ResponseWriter, r *http.Request) error {
	if err := h.svc.Star(r.Context(), h.userID(r.Context()), chi.URLParam(r, "symbol")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) unstar(w http.ResponseWriter, r *http.Request) error {
	if err := h.svc.Unstar(r.Context(), h.userID(r.Context()), chi.URLParam(r, "symbol")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

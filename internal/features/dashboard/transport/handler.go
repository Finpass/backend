// Package transport exposes /dashboard/home and /dashboard/portfolio.
package transport

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"shenzen/backend/internal/features/dashboard/application"
	"shenzen/backend/internal/platform/httpx"
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
	r.Route("/dashboard", func(r chi.Router) {
		r.Use(h.auth)
		r.Method(http.MethodGet, "/home", httpx.HandlerFunc(h.home))
		r.Method(http.MethodGet, "/portfolio", httpx.HandlerFunc(h.portfolio))
	})
}

func (h *Handler) home(w http.ResponseWriter, r *http.Request) error {
	v, err := h.svc.Home(r.Context(), h.userID(r.Context()), r.URL.Query().Get("range"))
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Dashboard fetched", v)
	return nil
}

func (h *Handler) portfolio(w http.ResponseWriter, r *http.Request) error {
	v, err := h.svc.Portfolio(r.Context(), h.userID(r.Context()), r.URL.Query().Get("range"))
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Portfolio dashboard fetched", v)
	return nil
}

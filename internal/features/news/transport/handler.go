// Package transport exposes /news.
package transport

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"shenzen/backend/internal/features/news/application"
	"shenzen/backend/internal/features/news/domain"
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
	r.Route("/news", func(r chi.Router) {
		r.Use(h.auth)
		r.Method(http.MethodGet, "/", httpx.HandlerFunc(h.list))
		r.Method(http.MethodGet, "/bookmarks", httpx.HandlerFunc(h.bookmarks))
		r.Method(http.MethodPost, "/{id}/bookmark", httpx.HandlerFunc(h.bookmark))
		r.Method(http.MethodDelete, "/{id}/bookmark", httpx.HandlerFunc(h.unbookmark))
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	f, err := h.svc.List(r.Context(), h.userID(r.Context()), domain.Filter{Category: q.Get("category"), Q: q.Get("q"), Limit: limit, Cursor: q.Get("cursor")})
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "News fetched", f)
	return nil
}

func (h *Handler) bookmarks(w http.ResponseWriter, r *http.Request) error {
	items, err := h.svc.Bookmarked(r.Context(), h.userID(r.Context()))
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Bookmarks fetched", items)
	return nil
}

func (h *Handler) bookmark(w http.ResponseWriter, r *http.Request) error {
	if err := h.svc.Bookmark(r.Context(), h.userID(r.Context()), chi.URLParam(r, "id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) unbookmark(w http.ResponseWriter, r *http.Request) error {
	if err := h.svc.Unbookmark(r.Context(), h.userID(r.Context()), chi.URLParam(r, "id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

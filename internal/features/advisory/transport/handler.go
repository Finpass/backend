// Package transport exposes /advisory.
package transport

import (
	"context"
	"net/http"
	"regexp"
	"strconv"

	"github.com/go-chi/chi/v5"

	"shenzen/backend/internal/features/advisory/application"
	"shenzen/backend/internal/features/advisory/domain"
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
	r.Route("/advisory", func(r chi.Router) {
		r.Use(h.auth)
		r.Method(http.MethodPost, "/chat", httpx.HandlerFunc(h.chat))
		r.Method(http.MethodGet, "/runs/{runId}/turns", httpx.HandlerFunc(h.turns))
		r.Method(http.MethodGet, "/context", httpx.HandlerFunc(h.context))
		r.Method(http.MethodGet, "/sessions", httpx.HandlerFunc(h.sessions))
		r.Method(http.MethodGet, "/sessions/{id}", httpx.HandlerFunc(h.session))
		r.Method(http.MethodDelete, "/sessions/{id}", httpx.HandlerFunc(h.remove))
	})
}

type chatBody struct {
	SessionID string   `json:"sessionId" validate:"omitempty,uuid"`
	Message   string   `json:"message" validate:"required,min=1,max=1000"`
	Lang      string   `json:"lang" validate:"required,oneof=id en"`
	RunID     string   `json:"runId" validate:"omitempty,uuid"`
	Compare   []string `json:"compare" validate:"omitempty,max=4,dive,min=1,max=24"`
}

func (h *Handler) chat(w http.ResponseWriter, r *http.Request) error {
	b, err := httpx.Decode[chatBody](w, r)
	if err != nil {
		return err
	}
	out, err := h.svc.ChatAbout(r.Context(), h.userID(r.Context()), b.SessionID, b.Message, b.Lang, b.RunID, b.Compare)
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Answer generated", out)
	return nil
}

func (h *Handler) context(w http.ResponseWriter, r *http.Request) error {
	uc, err := h.svc.Context(r.Context(), h.userID(r.Context()))
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Context fetched", uc)
	return nil
}

func (h *Handler) sessions(w http.ResponseWriter, r *http.Request) error {
	items, err := h.svc.Sessions(r.Context(), h.userID(r.Context()))
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Sessions fetched", map[string]any{"items": items})
	return nil
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// idParam returns "" for a malformed id, which no session can have: the caller answers 404 like any unknown session.
func idParam(r *http.Request) string {
	if id := chi.URLParam(r, "id"); uuidRe.MatchString(id) {
		return id
	}
	return ""
}

func (h *Handler) session(w http.ResponseWriter, r *http.Request) error {
	id := idParam(r)
	if id == "" {
		return domain.ErrNotFound
	}
	out, err := h.svc.Session(r.Context(), h.userID(r.Context()), id)
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Session fetched", out)
	return nil
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) error {
	id := idParam(r)
	if id == "" {
		return domain.ErrNotFound
	}
	if err := h.svc.Delete(r.Context(), h.userID(r.Context()), id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// turns serves the live progress steps of a reply being generated; unknown or malformed run ids yield an empty list.
func (h *Handler) turns(w http.ResponseWriter, r *http.Request) error {
	run := chi.URLParam(r, "runId")
	if !uuidRe.MatchString(run) {
		httpx.OK(w, http.StatusOK, "Turns fetched", map[string]any{"items": []domain.TurnEvent{}})
		return nil
	}
	since, _ := strconv.Atoi(r.URL.Query().Get("since"))
	items, err := h.svc.Turns(r.Context(), h.userID(r.Context()), run, max(since, 0))
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Turns fetched", map[string]any{"items": items})
	return nil
}

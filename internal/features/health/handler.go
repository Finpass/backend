package health

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"shenzen/backend/internal/platform/httpx"
	"shenzen/backend/internal/shared/apperr"
)

type Pinger interface {
	Ping(ctx context.Context) error
}

type Handler struct{ deps map[string]Pinger }

func New(deps map[string]Pinger) *Handler { return &Handler{deps: deps} }

func (h *Handler) Routes(r chi.Router) {
	r.Method(http.MethodGet, "/healthz", httpx.HandlerFunc(h.live))
	r.Method(http.MethodGet, "/readyz", httpx.HandlerFunc(h.ready))
}

func (h *Handler) live(w http.ResponseWriter, _ *http.Request) error {
	httpx.OK(w, http.StatusOK, "ok", nil)
	return nil
}

func (h *Handler) ready(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	for name, d := range h.deps {
		if err := d.Ping(ctx); err != nil {
			return apperr.Wrap(apperr.Unavailable, name+" unavailable", err)
		}
	}
	httpx.OK(w, http.StatusOK, "ready", nil)
	return nil
}

package transport

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"shenzen/backend/internal/features/note/application"
	"shenzen/backend/internal/features/note/domain"
	"shenzen/backend/internal/platform/httpx"
)

type Handler struct{ svc *application.Service }

func New(svc *application.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r chi.Router) {
	r.Method(http.MethodPost, "/notes", httpx.HandlerFunc(h.create))
	r.Method(http.MethodGet, "/notes/{id}", httpx.HandlerFunc(h.get))
}

type createRequest struct {
	Title string `json:"title" validate:"required,max=120"`
	Body  string `json:"body" validate:"max=10000"`
}

type noteResponse struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

func toResponse(n domain.Note) noteResponse {
	return noteResponse{ID: n.ID, Title: n.Title, Body: n.Body, CreatedAt: n.CreatedAt}
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[createRequest](w, r)
	if err != nil {
		return err
	}
	n, err := h.svc.Create(r.Context(), application.CreateCommand{Title: req.Title, Body: req.Body})
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusCreated, "Note created", toResponse(n))
	return nil
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	n, err := h.svc.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Note fetched", toResponse(n))
	return nil
}

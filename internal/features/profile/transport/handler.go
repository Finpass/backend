package transport

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"shenzen/backend/internal/features/profile/application"
	"shenzen/backend/internal/platform/audit"
	"shenzen/backend/internal/platform/httpx"
)

type Handler struct {
	svc    *application.Service
	auth   func(http.Handler) http.Handler
	userID func(context.Context) string
	audit  *audit.Recorder // nil records nothing
}

// Audited makes the handler record consent changes in the security trail.
func (h *Handler) Audited(r *audit.Recorder) *Handler { h.audit = r; return h }

// auth and userID come from the auth slice via the composition root, so slices never import each other.
func New(svc *application.Service, auth func(http.Handler) http.Handler, userID func(context.Context) string) *Handler {
	return &Handler{svc: svc, auth: auth, userID: userID}
}

func (h *Handler) Routes(r chi.Router) {
	r.Route("/profile", func(r chi.Router) {
		r.Use(h.auth)
		r.Method(http.MethodGet, "/questionnaire", httpx.HandlerFunc(h.questionnaire))
		r.Method(http.MethodGet, "/", httpx.HandlerFunc(h.get))
		r.Method(http.MethodPost, "/consent", httpx.HandlerFunc(h.consent))
		r.Method(http.MethodPut, "/answers", httpx.HandlerFunc(h.saveAnswers))
		r.Method(http.MethodPut, "/cash", httpx.HandlerFunc(h.cash))
		r.Method(http.MethodPost, "/submit", httpx.HandlerFunc(h.submit))
		r.Method(http.MethodGet, "/result", httpx.HandlerFunc(h.result))
		r.Method(http.MethodPost, "/retake", httpx.HandlerFunc(h.retake))
		r.Method(http.MethodDelete, "/", httpx.HandlerFunc(h.delete))
	})
}

type consentRequest struct {
	Accepted      bool `json:"accepted" validate:"eq=true"`
	WhatsappOptIn bool `json:"whatsappOptIn"`
}

type answersRequest struct {
	Section string         `json:"section"`
	Answers map[string]any `json:"answers"`
	OptOut  []string       `json:"optOut"`
}

// locale is "en" only when the first language tag says so; Indonesian is the default.
func locale(r *http.Request) string {
	tag := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Accept-Language"), ",", 2)[0]))
	if tag == "en" || strings.HasPrefix(tag, "en-") {
		return "en"
	}
	return "id"
}

func (h *Handler) questionnaire(w http.ResponseWriter, _ *http.Request) error {
	httpx.OK(w, http.StatusOK, "Questionnaire fetched", h.svc.Questionnaire())
	return nil
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	v, err := h.svc.Get(r.Context(), h.userID(r.Context()))
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Profile fetched", v)
	return nil
}

func (h *Handler) consent(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[consentRequest](w, r)
	if err != nil {
		return err
	}
	c, err := h.svc.Consent(r.Context(), h.userID(r.Context()), req.WhatsappOptIn)
	if err != nil {
		return err
	}
	h.audit.Record(r.Context(), audit.Event{Actor: h.userID(r.Context()), Action: "profile.consent", Meta: map[string]any{"whatsappOptIn": req.WhatsappOptIn}})
	httpx.OK(w, http.StatusOK, "Consent recorded", c)
	return nil
}

func (h *Handler) saveAnswers(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[answersRequest](w, r)
	if err != nil {
		return err
	}
	saved, err := h.svc.SaveAnswers(r.Context(), h.userID(r.Context()), application.SaveCommand{
		Section: req.Section, Answers: req.Answers, OptOut: req.OptOut,
	})
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Answers saved", saved)
	return nil
}

func (h *Handler) submit(w http.ResponseWriter, r *http.Request) error {
	res, err := h.svc.Submit(r.Context(), h.userID(r.Context()), locale(r))
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusCreated, "Profile assessed", res)
	return nil
}

func (h *Handler) result(w http.ResponseWriter, r *http.Request) error {
	res, err := h.svc.Result(r.Context(), h.userID(r.Context()))
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Result fetched", res)
	return nil
}

func (h *Handler) retake(w http.ResponseWriter, r *http.Request) error {
	v, err := h.svc.Retake(r.Context(), h.userID(r.Context()))
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Retake started", v)
	return nil
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) error {
	if err := h.svc.Delete(r.Context(), h.userID(r.Context())); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

type cashRequest struct {
	Amount int64 `json:"amount" validate:"min=0"`
}

// cash sets the liquid savings that also appear as the Cash row of the portfolio page.
func (h *Handler) cash(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[cashRequest](w, r)
	if err != nil {
		return err
	}
	if err := h.svc.UpdateCash(r.Context(), h.userID(r.Context()), req.Amount); err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Cash updated", map[string]int64{"amount": req.Amount})
	return nil
}

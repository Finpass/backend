package transport

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"shenzen/backend/internal/features/auth/application"
	"shenzen/backend/internal/platform/httpx"
)

type Handler struct {
	svc    *application.Service
	secure bool
}

func New(svc *application.Service, secureCookies bool) *Handler {
	return &Handler{svc: svc, secure: secureCookies}
}

func (h *Handler) Routes(r chi.Router) {
	r.Route("/auth", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(httpx.RateLimit(30, time.Minute))
			r.Method(http.MethodPost, "/register", httpx.HandlerFunc(h.register))
			r.Method(http.MethodPost, "/verify-email", httpx.HandlerFunc(h.verifyEmail))
			r.Method(http.MethodPost, "/resend-otp", httpx.HandlerFunc(h.resendOTP))
			r.Method(http.MethodPost, "/login", httpx.HandlerFunc(h.login))
			r.Method(http.MethodPost, "/google", httpx.HandlerFunc(h.google))
			r.Method(http.MethodPost, "/forgot-password", httpx.HandlerFunc(h.forgotPassword))
			r.Method(http.MethodPost, "/reset-password", httpx.HandlerFunc(h.resetPassword))
		})
		r.Method(http.MethodPost, "/refresh", httpx.HandlerFunc(h.refresh))
		r.Method(http.MethodPost, "/logout", httpx.HandlerFunc(h.logout))
		r.With(h.RequireAuth).Method(http.MethodGet, "/me", httpx.HandlerFunc(h.me))
	})
}

func (h *Handler) startSession(w http.ResponseWriter, res application.Result, status int, msg string) {
	h.setRefreshCookie(w, res.Tokens)
	httpx.OK(w, status, msg, toSession(res))
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[registerRequest](w, r)
	if err != nil {
		return err
	}
	email, err := h.svc.Register(r.Context(), application.RegisterCommand{Name: req.Name, Email: req.Email, Password: req.Password})
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusCreated, "Verification code sent", pendingResponse{Email: email})
	return nil
}

func (h *Handler) verifyEmail(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[verifyRequest](w, r)
	if err != nil {
		return err
	}
	res, err := h.svc.VerifyEmail(r.Context(), application.VerifyCommand{Email: req.Email, Code: req.Code, UserAgent: r.UserAgent()})
	if err != nil {
		return err
	}
	h.startSession(w, res, http.StatusOK, "Email verified")
	return nil
}

func (h *Handler) resendOTP(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[emailRequest](w, r)
	if err != nil {
		return err
	}
	if err := h.svc.ResendVerification(r.Context(), req.Email); err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "If the account exists, a new code was sent", nil)
	return nil
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[loginRequest](w, r)
	if err != nil {
		return err
	}
	res, err := h.svc.Login(r.Context(), application.LoginCommand{Email: req.Email, Password: req.Password, Remember: req.Remember, UserAgent: r.UserAgent()})
	if err != nil {
		return err
	}
	h.startSession(w, res, http.StatusOK, "Signed in")
	return nil
}

func (h *Handler) google(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[googleRequest](w, r)
	if err != nil {
		return err
	}
	res, err := h.svc.LoginWithGoogle(r.Context(), application.GoogleCommand{AccessToken: req.AccessToken, Remember: req.Remember, UserAgent: r.UserAgent()})
	if err != nil {
		return err
	}
	h.startSession(w, res, http.StatusOK, "Signed in with Google")
	return nil
}

func (h *Handler) forgotPassword(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[emailRequest](w, r)
	if err != nil {
		return err
	}
	if err := h.svc.ForgotPassword(r.Context(), req.Email); err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "If the account exists, a reset code was sent", nil)
	return nil
}

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[resetRequest](w, r)
	if err != nil {
		return err
	}
	if err := h.svc.ResetPassword(r.Context(), application.ResetCommand{Email: req.Email, Code: req.Code, NewPassword: req.NewPassword}); err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Password updated", nil)
	return nil
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) error {
	raw, err := refreshCookie(r)
	if err != nil {
		return err
	}
	res, err := h.svc.Refresh(r.Context(), raw, r.UserAgent())
	if err != nil {
		h.clearRefreshCookie(w)
		return err
	}
	h.startSession(w, res, http.StatusOK, "Session refreshed")
	return nil
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) error {
	if raw, err := refreshCookie(r); err == nil {
		if err := h.svc.Logout(r.Context(), raw); err != nil {
			return err
		}
	}
	h.clearRefreshCookie(w)
	httpx.OK(w, http.StatusOK, "Signed out", nil)
	return nil
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) error {
	u, err := h.svc.Me(r.Context(), UserID(r.Context()))
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Profile fetched", toUser(u))
	return nil
}

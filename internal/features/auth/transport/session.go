package transport

import (
	"context"
	"net/http"
	"strings"

	"shenzen/backend/internal/features/auth/application"
	"shenzen/backend/internal/features/auth/domain"
	"shenzen/backend/internal/platform/httpx"
)

const (
	refreshCookieName = "refresh_token"
	refreshCookiePath = "/api/v1/auth"
)

type userKey struct{}

func UserID(ctx context.Context) string {
	id, _ := ctx.Value(userKey{}).(string)
	return id
}

func (h *Handler) setRefreshCookie(w http.ResponseWriter, t application.Tokens) {
	http.SetCookie(w, &http.Cookie{
		Name: refreshCookieName, Value: t.Refresh, Path: refreshCookiePath, Expires: t.RefreshExpiresAt,
		HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: refreshCookieName, Value: "", Path: refreshCookiePath, MaxAge: -1,
		HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode,
	})
}

func refreshCookie(r *http.Request) (string, error) {
	c, err := r.Cookie(refreshCookieName)
	if err != nil || c.Value == "" {
		return "", domain.ErrInvalidSession
	}
	return c.Value, nil
}

func (h *Handler) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			httpx.WriteError(w, r, domain.ErrInvalidSession)
			return
		}
		id, err := h.svc.Authenticate(token)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, id)))
	})
}

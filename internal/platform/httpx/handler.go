package httpx

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"shenzen/backend/internal/shared/apperr"
)

type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

func (h HandlerFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h(w, r); err != nil {
		WriteError(w, r, err)
	}
}

func NewRouter(corsOrigins []string) *chi.Mux {
	r := chi.NewRouter()
	r.Use(RequestIDMiddleware, Recover, SecurityHeaders, AccessLog, CORS(corsOrigins), RejectNUL)
	r.NotFound(HandlerFunc(func(http.ResponseWriter, *http.Request) error {
		return errNotFound
	}).ServeHTTP)
	r.MethodNotAllowed(HandlerFunc(func(http.ResponseWriter, *http.Request) error {
		return errMethodNotAllowed
	}).ServeHTTP)
	return r
}

// RejectNUL answers 400 when the path or the query holds a NUL byte (also as %00). PostgreSQL refuses it in
// text, so it would otherwise surface as a 500 from deep inside a handler.
func RejectNUL(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.ContainsRune(r.URL.Path, 0) || strings.ContainsRune(r.URL.RawQuery, 0) || strings.Contains(strings.ToLower(r.URL.RawQuery), "%00") {
			WriteError(w, r, apperr.New(apperr.BadRequest, "Invalid characters in request"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

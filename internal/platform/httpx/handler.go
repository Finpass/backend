package httpx

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

func (h HandlerFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h(w, r); err != nil {
		WriteError(w, r, err)
	}
}

func NewRouter(corsOrigins []string) *chi.Mux {
	r := chi.NewRouter()
	r.Use(RequestIDMiddleware, Recover, AccessLog, CORS(corsOrigins))
	r.NotFound(HandlerFunc(func(http.ResponseWriter, *http.Request) error {
		return errNotFound
	}).ServeHTTP)
	r.MethodNotAllowed(HandlerFunc(func(http.ResponseWriter, *http.Request) error {
		return errMethodNotAllowed
	}).ServeHTTP)
	return r
}

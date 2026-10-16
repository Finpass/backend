package httpx

import "shenzen/backend/internal/shared/apperr"

var (
	errNotFound         = apperr.New(apperr.NotFound, "Route not found")
	errMethodNotAllowed = apperr.New(apperr.MethodNotAllowed, "Method not allowed")
)

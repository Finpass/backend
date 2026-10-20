package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"shenzen/backend/internal/platform/errorreport"
	"shenzen/backend/internal/shared/apperr"
)

type successBody struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

type errorBody struct {
	Success   bool        `json:"success"`
	Error     errorDetail `json:"error"`
	RequestID string      `json:"requestId,omitempty"`
}

type errorDetail struct {
	Code    apperr.Code     `json:"code"`
	Message string          `json:"message"`
	Details []apperr.Detail `json:"details"`
}

var statusByCode = map[apperr.Code]int{
	apperr.BadRequest:       http.StatusBadRequest,
	apperr.ValidationFailed: http.StatusUnprocessableEntity,
	apperr.Unauthorized:     http.StatusUnauthorized,
	apperr.Forbidden:        http.StatusForbidden,
	apperr.NotFound:         http.StatusNotFound,
	apperr.MethodNotAllowed: http.StatusMethodNotAllowed,
	apperr.Conflict:         http.StatusConflict,
	apperr.Unprocessable:    http.StatusUnprocessableEntity,
	apperr.TooManyRequests:  http.StatusTooManyRequests,
	apperr.Internal:         http.StatusInternalServerError,
	apperr.UpstreamFailed:   http.StatusBadGateway,
	apperr.Unavailable:      http.StatusServiceUnavailable,
	apperr.UpstreamTimeout:  http.StatusGatewayTimeout,
}

func StatusOf(code apperr.Code) int {
	if s, ok := statusByCode[code]; ok {
		return s
	}
	return http.StatusInternalServerError
}

func OK(w http.ResponseWriter, status int, message string, data any) {
	writeJSON(w, status, successBody{Success: true, Message: message, Data: data})
}

func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	ae, ok := apperr.As(err)
	if !ok {
		ae = apperr.Wrap(apperr.Internal, "Internal server error", err)
	}
	status := StatusOf(ae.Code)

	message, details := ae.Message, ae.Details
	if status >= 500 {
		slog.ErrorContext(r.Context(), "request failed", "code", ae.Code, "err", ae, "requestId", RequestID(r.Context()))
		errorreport.Capture(ae, string(ae.Code), RequestID(r.Context()))
		if ae.Code == apperr.Internal {
			message = "Internal server error"
		}
	}
	if details == nil {
		details = []apperr.Detail{}
	}

	writeJSON(w, status, errorBody{
		Error:     errorDetail{Code: ae.Code, Message: message, Details: details},
		RequestID: RequestID(r.Context()),
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

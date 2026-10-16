package apperr

import (
	"errors"
	"fmt"
)

type Code string

const (
	BadRequest       Code = "BAD_REQUEST"
	ValidationFailed Code = "VALIDATION_FAILED"
	Unauthorized     Code = "UNAUTHORIZED"
	Forbidden        Code = "FORBIDDEN"
	NotFound         Code = "NOT_FOUND"
	MethodNotAllowed Code = "METHOD_NOT_ALLOWED"
	Conflict         Code = "CONFLICT"
	Unprocessable    Code = "UNPROCESSABLE"
	TooManyRequests  Code = "TOO_MANY_REQUESTS"
	Internal         Code = "INTERNAL"
	UpstreamFailed   Code = "UPSTREAM_FAILED"
	Unavailable      Code = "UNAVAILABLE"
	UpstreamTimeout  Code = "UPSTREAM_TIMEOUT"
)

type Detail struct {
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

type Error struct {
	Code    Code
	Message string
	Details []Detail
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

func New(code Code, message string) *Error { return &Error{Code: code, Message: message} }

func Wrap(code Code, message string, err error) *Error {
	return &Error{Code: code, Message: message, Err: err}
}

func Validation(details ...Detail) *Error {
	return &Error{Code: ValidationFailed, Message: "Validation failed", Details: details}
}

func As(err error) (*Error, bool) {
	var e *Error
	return e, errors.As(err, &e)
}

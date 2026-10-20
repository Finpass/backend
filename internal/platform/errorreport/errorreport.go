// Package errorreport sends unexpected server errors to Sentry. It is off unless SENTRY_DSN is set.
//
// Only the error, its stack, the status code and the request id leave the process: no request, user, header,
// body or breadcrumb is attached, because they can hold personal and financial data.
package errorreport

import (
	"log/slog"
	"time"

	"github.com/getsentry/sentry-go"
)

var enabled bool

// Init starts reporting. It returns a flush function to call before the process exits. A bad DSN is logged, not fatal.
func Init(dsn, env, release string) func() {
	if dsn == "" {
		slog.Info("error reporting off (set SENTRY_DSN to turn it on)")
		return func() {}
	}
	err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		Environment:      env,
		Release:          release,
		SendDefaultPII:   false,
		AttachStacktrace: true,
		BeforeSend:       scrub,
	})
	if err != nil {
		slog.Error("sentry setup failed; continuing without it", "err", err)
		return func() {}
	}
	enabled = true
	slog.Info("error reporting on")
	return func() { sentry.Flush(2 * time.Second) }
}

// scrub drops everything that could identify a person; the request id tag is kept for lookup.
func scrub(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	event.Request = nil
	event.User = sentry.User{}
	event.Breadcrumbs = nil
	event.Contexts = nil
	return event
}

// Capture reports a server-side failure with the request id so it can be matched to the log line.
func Capture(err error, code, requestID string) {
	if !enabled || err == nil {
		return
	}
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("code", code)
		scope.SetTag("request_id", requestID)
		sentry.CaptureException(err)
	})
}

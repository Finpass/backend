package errorreport

import (
	"errors"
	"testing"

	"github.com/getsentry/sentry-go"
)

func TestScrubRemovesIdentifyingData(t *testing.T) {
	ev := &sentry.Event{
		Request:     &sentry.Request{URL: "http://x/api?email=a@b.c", Data: "gaji 20 juta"},
		User:        sentry.User{Email: "a@b.c", IPAddress: "1.2.3.4"},
		Breadcrumbs: []*sentry.Breadcrumb{{Message: "m"}},
		Contexts:    map[string]sentry.Context{"device": {"a": 1}},
		Tags:        map[string]string{"request_id": "abc"},
	}
	got := scrub(ev, nil)
	if got.Request != nil || got.User.Email != "" || got.User.IPAddress != "" || got.Breadcrumbs != nil || got.Contexts != nil {
		t.Fatalf("event not scrubbed: %+v", got)
	}
	if got.Tags["request_id"] != "abc" {
		t.Fatal("request id tag must stay")
	}
}

func TestOffWithoutDSNAndBadDSNDoNotPanic(t *testing.T) {
	Init("", "test", "x")()
	Init("not-a-dsn", "test", "x")()
	Capture(errors.New("boom"), "INTERNAL", "id") // no-op when disabled
}

package apperr_test

import (
	"errors"
	"fmt"
	"testing"

	"shenzen/backend/internal/shared/apperr"
)

func TestAsFindsWrappedError(t *testing.T) {
	cause := errors.New("db down")
	err := fmt.Errorf("load: %w", apperr.Wrap(apperr.Unavailable, "Database unavailable", cause))

	got, ok := apperr.As(err)
	if !ok || got.Code != apperr.Unavailable {
		t.Fatalf("want UNAVAILABLE, got %v (ok=%v)", got, ok)
	}
	if !errors.Is(err, cause) {
		t.Fatal("cause must stay reachable via errors.Is")
	}
}

func TestAsIgnoresPlainError(t *testing.T) {
	if _, ok := apperr.As(errors.New("boom")); ok {
		t.Fatal("plain error must not be an apperr")
	}
}

package domain_test

import (
	"strings"
	"testing"
	"time"

	"shenzen/backend/internal/features/note/domain"
	"shenzen/backend/internal/shared/apperr"
)

func TestNewRejectsBlankAndLongTitle(t *testing.T) {
	for _, title := range []string{"   ", strings.Repeat("a", 121)} {
		_, err := domain.New("1", title, "", time.Now())
		if ae, ok := apperr.As(err); !ok || ae.Code != apperr.ValidationFailed {
			t.Fatalf("title %q: want VALIDATION_FAILED, got %v", title, err)
		}
	}
}

func TestNewTrimsTitle(t *testing.T) {
	n, err := domain.New("1", "  hi  ", "", time.Now())
	if err != nil || n.Title != "hi" {
		t.Fatalf("n=%+v err=%v", n, err)
	}
}

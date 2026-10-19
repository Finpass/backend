package logger

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	var buf bytes.Buffer
	l := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{ReplaceAttr: redact}))
	l.Info("x", "email", "a@b.co", "phone", "+628123", "accessToken", "t0k3n", "path", "/ok")
	out := buf.String()
	for _, leak := range []string{"a@b.co", "+628123", "t0k3n"} {
		if strings.Contains(out, leak) {
			t.Fatalf("leaked %q in %s", leak, out)
		}
	}
	if !strings.Contains(out, "/ok") {
		t.Fatal("non-sensitive attributes must stay")
	}
}

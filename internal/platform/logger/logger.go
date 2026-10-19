package logger

import (
	"log/slog"
	"os"
	"strings"
)

// sensitive attribute names never reach the logs, whatever the call site passes.
var sensitive = []string{"password", "token", "secret", "authorization", "cookie", "otp", "code", "phone", "email", "apikey", "api_key"}

func redact(_ []string, a slog.Attr) slog.Attr {
	k := strings.ToLower(a.Key)
	for _, s := range sensitive {
		if strings.Contains(k, s) {
			return slog.String(a.Key, "[REDACTED]")
		}
	}
	return a
}

func New(env, level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl, ReplaceAttr: redact}
	if env == "production" {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env             string
	Port            string
	LogLevel        string
	DatabaseURL     string
	CORSOrigins     []string
	AIServiceURL    string
	InternalAPIKey  string
	ShutdownTimeout time.Duration
}

func (c Config) IsProduction() bool { return c.Env == "production" }

func Load() (Config, error) {
	c := Config{
		Env:             get("APP_ENV", "development"),
		Port:            get("PORT", "8080"),
		LogLevel:        get("LOG_LEVEL", "info"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		CORSOrigins:     split(get("CORS_ORIGINS", "http://localhost:3000")),
		AIServiceURL:    get("AI_SERVICE_URL", "http://localhost:8001"),
		InternalAPIKey:  os.Getenv("INTERNAL_API_KEY"),
		ShutdownTimeout: 15 * time.Second,
	}

	if v := os.Getenv("SHUTDOWN_TIMEOUT_SECONDS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT_SECONDS must be a positive integer, got %q", v)
		}
		c.ShutdownTimeout = time.Duration(n) * time.Second
	}

	return c, c.validate()
}

func (c Config) validate() error {
	var errs []error
	if c.IsProduction() {
		if c.DatabaseURL == "" {
			errs = append(errs, errors.New("DATABASE_URL is required in production"))
		}
		if c.InternalAPIKey == "" || c.InternalAPIKey == "development-internal-key" {
			errs = append(errs, errors.New("INTERNAL_API_KEY must be set to a real secret in production"))
		}
	}
	return errors.Join(errs...)
}

func get(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func split(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

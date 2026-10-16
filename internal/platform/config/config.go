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
	Auth            AuthConfig
}

type AuthConfig struct {
	JWTSecret      string
	AccessTTL      time.Duration
	RefreshTTL     time.Duration
	RememberTTL    time.Duration
	GoogleClientID string
	SMTPHost       string
	SMTPPort       string
	SMTPUser       string
	SMTPPassword   string
	MailFrom       string
}

const devJWTSecret = "development-jwt-secret-change-me-please-0123456789"

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
		Auth: AuthConfig{
			JWTSecret:      get("JWT_SECRET", devJWTSecret),
			AccessTTL:      15 * time.Minute,
			RefreshTTL:     24 * time.Hour,
			RememberTTL:    30 * 24 * time.Hour,
			GoogleClientID: os.Getenv("GOOGLE_CLIENT_ID"),
			SMTPHost:       os.Getenv("SMTP_HOST"),
			SMTPPort:       get("SMTP_PORT", "587"),
			SMTPUser:       os.Getenv("SMTP_USER"),
			SMTPPassword:   os.Getenv("SMTP_PASSWORD"),
			MailFrom:       os.Getenv("MAIL_FROM"),
		},
	}

	if v := os.Getenv("SHUTDOWN_TIMEOUT_SECONDS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT_SECONDS must be a positive integer, got %q", v)
		}
		c.ShutdownTimeout = time.Duration(n) * time.Second
	}

	var err error
	if c.Auth.AccessTTL, err = duration("ACCESS_TOKEN_TTL_MINUTES", c.Auth.AccessTTL, time.Minute); err != nil {
		return Config{}, err
	}
	if c.Auth.RefreshTTL, err = duration("REFRESH_TTL_HOURS", c.Auth.RefreshTTL, time.Hour); err != nil {
		return Config{}, err
	}
	if c.Auth.RememberTTL, err = duration("REMEMBER_TTL_DAYS", c.Auth.RememberTTL, 24*time.Hour); err != nil {
		return Config{}, err
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
		if c.Auth.JWTSecret == devJWTSecret {
			errs = append(errs, errors.New("JWT_SECRET must be set in production"))
		}
		if c.Auth.SMTPHost == "" || c.Auth.MailFrom == "" {
			errs = append(errs, errors.New("SMTP_HOST and MAIL_FROM are required in production"))
		}
	}
	if len(c.Auth.JWTSecret) < 32 {
		errs = append(errs, errors.New("JWT_SECRET must be at least 32 characters"))
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

func duration(key string, fallback, unit time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", key, v)
	}
	return time.Duration(n) * unit, nil
}

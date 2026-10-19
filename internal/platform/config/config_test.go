package config

import "testing"

func TestProductionRequiresSecrets(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("INTERNAL_API_KEY", "")
	if _, err := Load(); err == nil {
		t.Fatal("production without DATABASE_URL and INTERNAL_API_KEY must fail")
	}
}

func TestDevelopmentDefaults(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("PORT", "")
	c, err := Load()
	if err != nil || c.Port != "8080" || c.IsProduction() {
		t.Fatalf("unexpected defaults: %+v err=%v", c, err)
	}
}

func TestProductionRequiresDataKeys(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("INTERNAL_API_KEY", "a-real-secret")
	t.Setenv("JWT_SECRET", "0123456789012345678901234567890123456789")
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("MAIL_FROM", "no-reply@example.com")
	t.Setenv("DATA_KEK", "")
	if _, err := Load(); err == nil {
		t.Fatal("production without DATA_KEK and DATA_INDEX_KEY must fail")
	}
}

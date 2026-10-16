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

package config

import "testing"

func TestLoadUsesDefaults(t *testing.T) {
	t.Setenv("API_PORT", "")
	t.Setenv("DB_DSN", "")
	t.Setenv("JWT_SECRET", "")

	cfg := Load()
	if cfg.Port != "8080" {
		t.Fatalf("expected default port 8080, got %s", cfg.Port)
	}
	if cfg.DatabaseDSN == "" {
		t.Fatal("expected default database DSN")
	}
	if cfg.JWTSecret == "" {
		t.Fatal("expected default JWT secret")
	}
}

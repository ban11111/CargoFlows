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

func TestLoadReadsAIConfiguration(t *testing.T) {
	t.Setenv("CARGOFLOW_SECRETS_MASTER_KEY", "c2VjcmV0c2VjcmV0c2VjcmV0c2VjcmV0MTIzNDU2Nzg=")
	t.Setenv("OPENAI_BASE_URL", "https://example.test/v1")
	t.Setenv("AI_WORKER_DRY_RUN", "true")
	cfg := Load()
	if cfg.SecretsMasterKey == "" || cfg.OpenAIBaseURL != "https://example.test/v1" || !cfg.AIWorkerDryRun {
		t.Fatalf("unexpected AI config: %#v", cfg)
	}
}

package config

import (
	"testing"
	"time"
)

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
	t.Setenv("CARGOFLOWS_SECRETS_MASTER_KEY", "c2VjcmV0c2VjcmV0c2VjcmV0c2VjcmV0MTIzNDU2Nzg=")
	t.Setenv("OPENAI_BASE_URL", "https://example.test/v1")
	t.Setenv("AI_WORKER_DRY_RUN", "true")
	cfg := Load()
	if cfg.SecretsMasterKey == "" || cfg.OpenAIBaseURL != "https://example.test/v1" || !cfg.AIWorkerDryRun {
		t.Fatalf("unexpected AI config: %#v", cfg)
	}
}

func TestLoadUsesAndReadsOpenAITextDefaults(t *testing.T) {
	t.Setenv("OPENAI_TEXT_MODEL", "")
	t.Setenv("OPENAI_REASONING_EFFORT", "")
	t.Setenv("OPENAI_REQUEST_TIMEOUT", "")
	cfg := Load()
	if cfg.OpenAITextModel != "gpt-5.6-terra" || cfg.OpenAIReasoningEffort != "low" || cfg.OpenAIRequestTimeout != 300*time.Second {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}
	t.Setenv("OPENAI_TEXT_MODEL", "test-model")
	t.Setenv("OPENAI_REASONING_EFFORT", "medium")
	t.Setenv("OPENAI_REQUEST_TIMEOUT", "45s")
	cfg = Load()
	if cfg.OpenAITextModel != "test-model" || cfg.OpenAIReasoningEffort != "medium" || cfg.OpenAIRequestTimeout != 45*time.Second {
		t.Fatalf("unexpected configured values: %#v", cfg)
	}
}

func TestLoadUsesAndReadsOpenAIImageDefaults(t *testing.T) {
	t.Setenv("OPENAI_IMAGE_TOOL_MODEL", "")
	t.Setenv("OPENAI_IMAGE_REQUEST_TIMEOUT", "")
	cfg := Load()
	if cfg.OpenAIImageToolModel != "gpt-5.6" || cfg.OpenAIImageRequestTimeout != 600*time.Second {
		t.Fatalf("unexpected image defaults: %#v", cfg)
	}
	t.Setenv("OPENAI_IMAGE_TOOL_MODEL", "test-image-model")
	t.Setenv("OPENAI_IMAGE_REQUEST_TIMEOUT", "90s")
	cfg = Load()
	if cfg.OpenAIImageToolModel != "test-image-model" || cfg.OpenAIImageRequestTimeout != 90*time.Second {
		t.Fatalf("unexpected configured image values: %#v", cfg)
	}
}

func TestLoadUsesAndReadsPrivateGeneratedImageBucket(t *testing.T) {
	t.Setenv("MINIO_AI_BUCKET", "")
	if cfg := Load(); cfg.MinIOAIBucket != "cargoflows-ai-private" {
		t.Fatalf("unexpected generated-image bucket default: %#v", cfg)
	}
	t.Setenv("MINIO_AI_BUCKET", "private-test-images")
	if cfg := Load(); cfg.MinIOAIBucket != "private-test-images" {
		t.Fatalf("unexpected configured generated-image bucket: %#v", cfg)
	}
}

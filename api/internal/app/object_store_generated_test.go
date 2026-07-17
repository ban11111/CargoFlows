package app

import (
	"testing"

	"cargoflow/api/internal/config"
)

func TestGeneratedBucketUsesSeparatePrivateDefault(t *testing.T) {
	t.Setenv("MINIO_AI_BUCKET", "")
	if cfg := config.Load(); cfg.MinIOAIBucket != "cargoflow-ai-private" {
		t.Fatalf("MINIO_AI_BUCKET default = %q, want private generated-image bucket", cfg.MinIOAIBucket)
	}
	if generatedBucketPolicy != "" {
		t.Fatalf("generated bucket policy must remain private, got %q", generatedBucketPolicy)
	}
}

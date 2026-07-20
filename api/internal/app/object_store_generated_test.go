package app

import (
	"testing"

	"cargoflows/api/internal/config"
)

func TestNewObjectStoreRejectsEmptyOrSharedGeneratedBucket(t *testing.T) {
	for name, cfg := range map[string]config.Config{
		"empty source":    {MinIOEndpoint: "minio:9000", MinIOPublicEndpoint: "minio:9000", MinIOBucket: "", MinIOAIBucket: "private"},
		"empty generated": {MinIOEndpoint: "minio:9000", MinIOPublicEndpoint: "minio:9000", MinIOBucket: "source", MinIOAIBucket: ""},
		"shared bucket":   {MinIOEndpoint: "minio:9000", MinIOPublicEndpoint: "minio:9000", MinIOBucket: "source", MinIOAIBucket: "source"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := newObjectStore(cfg); err == nil {
				t.Fatal("newObjectStore() accepted unsafe source/generated bucket configuration")
			}
		})
	}
}

func TestGeneratedBucketUsesSeparatePrivateDefault(t *testing.T) {
	t.Setenv("MINIO_AI_BUCKET", "")
	if cfg := config.Load(); cfg.MinIOAIBucket != "cargoflows-ai-private" {
		t.Fatalf("MINIO_AI_BUCKET default = %q, want private generated-image bucket", cfg.MinIOAIBucket)
	}
	if generatedBucketPolicy != "" {
		t.Fatalf("generated bucket policy must remain private, got %q", generatedBucketPolicy)
	}
	if sourceBucketPolicy != "" {
		t.Fatalf("source bucket policy must remain private, got %q", sourceBucketPolicy)
	}
}

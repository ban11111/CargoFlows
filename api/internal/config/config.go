package config

import (
	"os"
	"time"
)

type Config struct {
	Port                      string
	DatabaseDSN               string
	JWTSecret                 string
	AppEnv                    string
	MinIOEndpoint             string
	MinIOPublicEndpoint       string
	MinIOAccessKey            string
	MinIOSecretKey            string
	MinIOBucket               string
	MinIOAIBucket             string
	SecretsMasterKey          string
	OpenAIBaseURL             string
	OpenAITextModel           string
	OpenAIImageToolModel      string
	OpenAIReasoningEffort     string
	OpenAIRequestTimeout      time.Duration
	OpenAIImageRequestTimeout time.Duration
	AIWorkerDryRun            bool
	AIWorkerPollInterval      time.Duration
}

func Load() Config {
	return Config{
		Port:                      getEnv("API_PORT", "8080"),
		DatabaseDSN:               getEnv("DB_DSN", "cargoflows:cargoflows@tcp(127.0.0.1:3306)/cargoflows?charset=utf8mb4&parseTime=True&loc=Local"),
		JWTSecret:                 getEnv("JWT_SECRET", "dev-secret-change-me"),
		AppEnv:                    getEnv("APP_ENV", "development"),
		MinIOEndpoint:             getEnv("MINIO_ENDPOINT", "127.0.0.1:9000"),
		MinIOPublicEndpoint:       getEnv("MINIO_PUBLIC_ENDPOINT", "127.0.0.1:9000"),
		MinIOAccessKey:            getEnv("MINIO_ROOT_USER", "cargoflows"),
		MinIOSecretKey:            getEnv("MINIO_ROOT_PASSWORD", "cargoflows123"),
		MinIOBucket:               getEnv("MINIO_BUCKET", "cargoflows"),
		MinIOAIBucket:             getEnv("MINIO_AI_BUCKET", "cargoflows-ai-private"),
		SecretsMasterKey:          getEnv("CARGOFLOWS_SECRETS_MASTER_KEY", ""),
		OpenAIBaseURL:             getEnv("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		OpenAITextModel:           getEnv("OPENAI_TEXT_MODEL", "gpt-5.6-terra"),
		OpenAIImageToolModel:      getEnv("OPENAI_IMAGE_TOOL_MODEL", "gpt-5.6"),
		OpenAIReasoningEffort:     getEnv("OPENAI_REASONING_EFFORT", "low"),
		OpenAIRequestTimeout:      getDurationEnv("OPENAI_REQUEST_TIMEOUT", 120*time.Second),
		OpenAIImageRequestTimeout: getDurationEnv("OPENAI_IMAGE_REQUEST_TIMEOUT", 180*time.Second),
		AIWorkerDryRun:            getEnv("AI_WORKER_DRY_RUN", "false") == "true",
		AIWorkerPollInterval:      getDurationEnv("AI_WORKER_POLL_INTERVAL", time.Second),
	}
}

func getEnv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

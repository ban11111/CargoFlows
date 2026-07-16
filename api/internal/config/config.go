package config

import "os"

type Config struct {
	Port                string
	DatabaseDSN         string
	JWTSecret           string
	AppEnv              string
	MinIOEndpoint       string
	MinIOPublicEndpoint string
	MinIOAccessKey      string
	MinIOSecretKey      string
	MinIOBucket         string
}

func Load() Config {
	return Config{
		Port:                getEnv("API_PORT", "8080"),
		DatabaseDSN:         getEnv("DB_DSN", "cargoflow:cargoflow@tcp(127.0.0.1:3306)/cargoflow?charset=utf8mb4&parseTime=True&loc=Local"),
		JWTSecret:           getEnv("JWT_SECRET", "dev-secret-change-me"),
		AppEnv:              getEnv("APP_ENV", "development"),
		MinIOEndpoint:       getEnv("MINIO_ENDPOINT", "127.0.0.1:9000"),
		MinIOPublicEndpoint: getEnv("MINIO_PUBLIC_ENDPOINT", "127.0.0.1:9000"),
		MinIOAccessKey:      getEnv("MINIO_ROOT_USER", "cargoflow"),
		MinIOSecretKey:      getEnv("MINIO_ROOT_PASSWORD", "cargoflow123"),
		MinIOBucket:         getEnv("MINIO_BUCKET", "cargoflow"),
	}
}

func getEnv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

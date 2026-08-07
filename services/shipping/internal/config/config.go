// Package config reads shipping service configuration from the environment.
package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	DSN                 string
	GRPCPort            string
	MinioEndpoint       string
	MinioPublicEndpoint string
	MinioAccessKey      string
	MinioSecretKey      string
	MinioBucket         string
	MinioUseSSL         bool
	RedisAddr           string
	ReminderInterval    time.Duration
	ReminderBatchSize   int32
}

func Load() Config {
	return Config{
		DSN:                 env("DB_DSN", "postgres://erp_shipping:erp_shipping_pw@localhost:5433/erp_shipping?sslmode=disable"),
		GRPCPort:            env("GRPC_PORT", "9009"),
		MinioEndpoint:       env("MINIO_ENDPOINT", "localhost:19000"),
		MinioPublicEndpoint: env("MINIO_PUBLIC_ENDPOINT", ""),
		MinioAccessKey:      env("MINIO_ACCESS_KEY", "erp"),
		MinioSecretKey:      env("MINIO_SECRET_KEY", "erp_dev_password"),
		MinioBucket:         env("MINIO_BUCKET", "erp-files"),
		MinioUseSSL:         env("MINIO_USE_SSL", "") == "true",
		RedisAddr:           env("REDIS_ADDR", ""),
		ReminderInterval:    durationEnv("SHIPPING_REMINDER_INTERVAL", 24*time.Hour),
		ReminderBatchSize:   int32(intEnv("SHIPPING_REMINDER_BATCH_SIZE", 100)),
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func intEnv(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

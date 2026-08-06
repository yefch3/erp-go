// Package config reads shipping service configuration from the environment.
package config

import "os"

type Config struct {
	DSN                 string
	GRPCPort            string
	MinioEndpoint       string
	MinioPublicEndpoint string
	MinioAccessKey      string
	MinioSecretKey      string
	MinioBucket         string
	MinioUseSSL         bool
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
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// Package config reads the service configuration from the environment.
package config

import "os"

type Config struct {
	DSN            string
	GRPCPort       string
	MasterdataAddr string
	MinioEndpoint  string
	// Where the browser reaches MinIO; presigned URLs are signed for it.
	MinioPublicEndpoint string
	MinioAccessKey      string
	MinioSecretKey      string
	MinioBucket         string
	MinioUseSSL         bool
}

func Load() Config {
	return Config{
		DSN:                 env("DB_DSN", "postgres://erp_product:erp_product_pw@localhost:5433/erp_product?sslmode=disable"),
		GRPCPort:            env("GRPC_PORT", "9004"),
		MasterdataAddr:      env("MASTERDATA_ADDR", "localhost:9002"),
		MinioEndpoint:       env("MINIO_ENDPOINT", "localhost:19000"),
		MinioPublicEndpoint: env("MINIO_PUBLIC_ENDPOINT", ""),
		MinioAccessKey:      env("MINIO_ACCESS_KEY", "erp"),
		MinioSecretKey:      env("MINIO_SECRET_KEY", "erp_dev_password"),
		MinioBucket:         env("MINIO_BUCKET", "erp-files"),
		MinioUseSSL:         env("MINIO_USE_SSL", "") == "true",
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Package config reads shipping service configuration from the environment.
package config

import "os"

type Config struct {
	DSN      string
	GRPCPort string
}

func Load() Config {
	return Config{
		DSN:      env("DB_DSN", "postgres://erp_shipping:erp_shipping_pw@localhost:5433/erp_shipping?sslmode=disable"),
		GRPCPort: env("GRPC_PORT", "9009"),
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

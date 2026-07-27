// Package config reads the service configuration from the environment.
package config

import "os"

type Config struct {
	DSN      string
	GRPCPort string
}

func Load() Config {
	return Config{
		DSN:      env("DB_DSN", "postgres://erp_masterdata:erp_masterdata_pw@localhost:5433/erp_masterdata?sslmode=disable"),
		GRPCPort: env("GRPC_PORT", "9002"),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

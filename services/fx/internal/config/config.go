// Package config reads the service configuration from the environment.
package config

import (
	"os"
	"strings"
	"time"
)

type Config struct {
	DSN           string
	GRPCPort      string
	FetchURL      string
	FetchSymbols  []string
	FetchInterval time.Duration
}

func Load() Config {
	return Config{
		DSN:      env("DB_DSN", "postgres://erp_fx:erp_fx_pw@localhost:5433/erp_fx?sslmode=disable"),
		GRPCPort: env("GRPC_PORT", "9003"),
		// frankfurter.app republishes ECB reference rates, working days only.
		FetchURL:      env("FX_FETCH_URL", "https://api.frankfurter.app/latest"),
		FetchSymbols:  strings.Split(env("FX_SYMBOLS", "CNY,EUR,GBP,JPY,HKD"), ","),
		FetchInterval: envDuration("FX_FETCH_INTERVAL", 6*time.Hour),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// Package config reads the service configuration from the environment.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DSN           string
	GRPCPort      string
	FetchURL      string
	FetchSymbols  []string
	FetchInterval time.Duration
	// How much history to pull at startup so the 汇率走势 chart has a curve
	// on the first day rather than after a year of uptime. 0 disables it.
	BackfillDays int
}

func Load() Config {
	return Config{
		DSN:      env("DB_DSN", "postgres://erp_fx:erp_fx_pw@localhost:5433/erp_fx?sslmode=disable"),
		GRPCPort: env("GRPC_PORT", "9003"),
		// frankfurter.app republishes ECB reference rates, working days only.
		FetchURL:      env("FX_FETCH_URL", "https://api.frankfurter.app/latest"),
		FetchSymbols:  strings.Split(env("FX_SYMBOLS", "CNY,EUR,GBP,JPY,HKD"), ","),
		FetchInterval: envDuration("FX_FETCH_INTERVAL", 6*time.Hour),
		// A year: enough for the longest range the chart offers, and one
		// request either way.
		BackfillDays: envInt("FX_BACKFILL_DAYS", 365),
	}
}

func envInt(key string, def int) int {
	v, err := strconv.Atoi(os.Getenv(key))
	if err != nil || v < 0 {
		return def
	}
	return v
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

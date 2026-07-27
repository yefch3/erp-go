// Package config reads the gateway configuration from the environment.
package config

import "os"

type Config struct {
	HTTPPort       string
	IAMAddr        string
	MasterdataAddr string
	FxAddr         string
	JWTSecret      string
}

func Load() Config {
	return Config{
		HTTPPort:       env("HTTP_PORT", "8080"),
		IAMAddr:        env("IAM_ADDR", "localhost:9001"),
		MasterdataAddr: env("MASTERDATA_ADDR", "localhost:9002"),
		FxAddr:         env("FX_ADDR", "localhost:9003"),
		// Must match iam's JWT_SECRET or every token fails validation.
		JWTSecret: env("JWT_SECRET", "dev-secret-change-in-production"),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

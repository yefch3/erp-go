// Package config reads the service configuration from the environment.
// Defaults target local development against deploy/docker-compose.infra.yml.
package config

import (
	"os"
	"time"
)

type Config struct {
	DSN                  string
	GRPCPort             string
	JWTSecret            string
	JWTTTL               time.Duration
	AdminInitialPassword string
	// The first tenant, seeded on an empty database. The domains are what the
	// login page routes on, so getting them right matters more than the name.
	CompanyName        string
	CompanyMailDomains string
	AdminEmail         string
}

func Load() Config {
	return Config{
		DSN:       env("DB_DSN", "postgres://erp_iam:erp_iam_pw@localhost:5433/erp_iam?sslmode=disable"),
		GRPCPort:  env("GRPC_PORT", "9001"),
		JWTSecret: env("JWT_SECRET", "dev-secret-change-in-production"),
		JWTTTL:    envDuration("JWT_TTL", 24*time.Hour),
		// Used only to bootstrap the very first account on an empty database.
		AdminInitialPassword: env("ADMIN_INITIAL_PASSWORD", "admin123"),
		CompanyName:          env("COMPANY_NAME", "Demo Company"),
		// Comma-separated: a Chinese exporter commonly holds both
		// example.com and example.com.cn, and both must reach one tenant.
		CompanyMailDomains: env("COMPANY_MAIL_DOMAINS", "example.com"),
		AdminEmail:         env("ADMIN_EMAIL", "admin@example.com"),
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

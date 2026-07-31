// Package config reads the service configuration from the environment.
package config

import (
	"os"
	"time"
)

type Config struct {
	DSN            string
	GRPCPort       string
	MasterdataAddr string
	IAMAddr        string
	// Provider selection. Only "dev" exists today: the sending service has
	// not been chosen and merchant onboarding has not happened, so nothing
	// can actually go out yet. Everything upstream is real.
	Provider string
	// Worker tuning. BatchSize doubles as the rate limit — providers cap
	// sends per second, and ignoring the cap is what produces the
	// "some succeeded, some failed" pattern this design exists to avoid.
	// Object storage for attachments and inline images.
	MinioEndpoint       string
	MinioPublicEndpoint string
	MinioAccessKey      string
	MinioSecretKey      string
	MinioBucket         string
	MinioUseSSL         bool

	BatchSize      int32
	SendDelay      time.Duration
	DecisionWindow time.Duration
}

func Load() Config {
	return Config{
		DSN:            env("DB_DSN", "postgres://erp_notification:erp_notification_pw@localhost:5433/erp_notification?sslmode=disable"),
		GRPCPort:       env("GRPC_PORT", "9010"),
		MasterdataAddr: env("MASTERDATA_ADDR", "localhost:9002"),
		IAMAddr:        env("IAM_ADDR", "localhost:9001"),
		Provider:       env("MAIL_PROVIDER", "dev"),
		MinioEndpoint:  env("MINIO_ENDPOINT", "localhost:19000"),
		// Browsers reach MinIO on the host port and SigV4 signs the host, so
		// presigned URLs must be signed for the address the browser will use.
		MinioPublicEndpoint: env("MINIO_PUBLIC_ENDPOINT", "localhost:19000"),
		MinioAccessKey:      env("MINIO_ACCESS_KEY", "erp"),
		MinioSecretKey:      env("MINIO_SECRET_KEY", "erp_dev_password"),
		MinioBucket:         env("MINIO_BUCKET", "erp-files"),
		MinioUseSSL:         os.Getenv("MINIO_USE_SSL") == "true",

		BatchSize:      int32(envInt("MAIL_BATCH_SIZE", 20)),
		SendDelay:      envDuration("MAIL_SEND_DELAY", 100*time.Millisecond),
		DecisionWindow: envDuration("MAIL_DECISION_WINDOW", 10*time.Minute),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		n := 0
		for _, r := range v {
			if r < '0' || r > '9' {
				return def
			}
			n = n*10 + int(r-'0')
		}
		if n > 0 {
			return n
		}
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

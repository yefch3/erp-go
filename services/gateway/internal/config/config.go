// Package config reads the gateway configuration from the environment.
package config

import (
	"os"
	"time"
)

type Config struct {
	HTTPPort        string
	IAMAddr         string
	MasterdataAddr  string
	FxAddr          string
	ApprovalAddr    string
	ProductAddr     string
	ExportAddr      string
	ProcurementAddr string
	InventoryAddr   string
	ShippingAddr    string
	MailAddr        string
	// Redis carries the live UI feed; see pkg/livefeed.
	RedisAddr string
	JWTSecret string
	// JWTTTL is the life of a token the gateway renews. It must match iam's
	// JWT_TTL — the two read the same variable so that setting it in one
	// place cannot leave renewal handing out tokens of a different length
	// from the ones login hands out.
	JWTTTL time.Duration
}

func Load() Config {
	return Config{
		HTTPPort:        env("HTTP_PORT", "8080"),
		IAMAddr:         env("IAM_ADDR", "localhost:9001"),
		MasterdataAddr:  env("MASTERDATA_ADDR", "localhost:9002"),
		FxAddr:          env("FX_ADDR", "localhost:9003"),
		ApprovalAddr:    env("APPROVAL_ADDR", "localhost:9005"),
		ProductAddr:     env("PRODUCT_ADDR", "localhost:9004"),
		ExportAddr:      env("EXPORT_ADDR", "localhost:9006"),
		ProcurementAddr: env("PROCUREMENT_ADDR", "localhost:9007"),
		InventoryAddr:   env("INVENTORY_ADDR", "localhost:9008"),
		ShippingAddr:    env("SHIPPING_ADDR", "localhost:9009"),
		MailAddr:        env("MAIL_ADDR", "localhost:9010"),
		RedisAddr:       env("REDIS_ADDR", "localhost:6380"),
		// Must match iam's JWT_SECRET or every token fails validation.
		JWTSecret: env("JWT_SECRET", "dev-secret-change-in-production"),
		JWTTTL:    envDuration("JWT_TTL", 12*time.Hour),
	}
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

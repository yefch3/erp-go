// Package config reads the service configuration from the environment.
package config

import "os"

type Config struct {
	DSN            string
	GRPCPort       string
	MasterdataAddr string
	ProductAddr    string
	FxAddr         string
	ApprovalAddr   string
}

func Load() Config {
	return Config{
		DSN:            env("DB_DSN", "postgres://erp_export:erp_export_pw@localhost:5433/erp_export?sslmode=disable"),
		GRPCPort:       env("GRPC_PORT", "9006"),
		MasterdataAddr: env("MASTERDATA_ADDR", "localhost:9002"),
		ProductAddr:    env("PRODUCT_ADDR", "localhost:9004"),
		FxAddr:         env("FX_ADDR", "localhost:9003"),
		ApprovalAddr:   env("APPROVAL_ADDR", "localhost:9005"),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

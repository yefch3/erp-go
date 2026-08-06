package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DB_DSN", "")
	t.Setenv("GRPC_PORT", "")

	cfg := Load()
	if cfg.GRPCPort != "9009" {
		t.Fatalf("GRPCPort = %q, want 9009", cfg.GRPCPort)
	}
	if cfg.DSN != "postgres://erp_shipping:erp_shipping_pw@localhost:5433/erp_shipping?sslmode=disable" {
		t.Fatalf("unexpected default DSN: %q", cfg.DSN)
	}
	if cfg.ReminderInterval != 24*time.Hour || cfg.ReminderBatchSize != 100 {
		t.Fatalf("unexpected reminder defaults: %+v", cfg)
	}
}

func TestLoadEnvironment(t *testing.T) {
	t.Setenv("DB_DSN", "postgres://example")
	t.Setenv("GRPC_PORT", "19009")
	t.Setenv("SHIPPING_REMINDER_INTERVAL", "15m")
	t.Setenv("SHIPPING_REMINDER_BATCH_SIZE", "25")

	cfg := Load()
	if cfg.DSN != "postgres://example" || cfg.GRPCPort != "19009" || cfg.ReminderInterval != 15*time.Minute || cfg.ReminderBatchSize != 25 {
		t.Fatalf("environment was not applied: %+v", cfg)
	}
}

// Package config reads the service configuration from the environment.
package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DSN          string
	GRPCPort     string
	IAMAddr      string
	KafkaBrokers []string
	// Topic every approval decision is published to.
	DecisionTopic string
	// Redis, for the live UI feed. Not the event bus; see pkg/livefeed.
	RedisAddr string
	// PurchaseOrderFallbackRoleID receives purchase approvals when the
	// submitter has no manager in the organisation tree.
	PurchaseOrderFallbackRoleID int64
}

func Load() Config {
	return Config{
		DSN:                         env("DB_DSN", "postgres://erp_approval:erp_approval_pw@localhost:5433/erp_approval?sslmode=disable"),
		GRPCPort:                    env("GRPC_PORT", "9005"),
		IAMAddr:                     env("IAM_ADDR", "localhost:9001"),
		KafkaBrokers:                strings.Split(env("KAFKA_BROKERS", "localhost:19092"), ","),
		DecisionTopic:               env("APPROVAL_TOPIC", "erp.approval.task.v1"),
		RedisAddr:                   env("REDIS_ADDR", "localhost:6380"),
		PurchaseOrderFallbackRoleID: envInt64("PURCHASE_ORDER_FALLBACK_ROLE_ID", 3),
	}
}

func envInt64(key string, def int64) int64 {
	value, err := strconv.ParseInt(env(key, ""), 10, 64)
	if err != nil || value <= 0 {
		return def
	}
	return value
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

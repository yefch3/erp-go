// Package config reads the service configuration from the environment.
package config

import (
	"os"
	"strings"
)

type Config struct {
	DSN          string
	GRPCPort     string
	KafkaBrokers []string
	// Procurement listens to inventory, not to export. What has to be bought
	// is whatever stock could not cover, and only inventory can say that.
	StockTopic    string
	ConsumerGroup string
	// Approval decisions turn a submitted order into a placed one.
	ApprovalTopic         string
	ApprovalConsumerGroup string
	// Receipts leave here for inventory, which applies the stock increase.
	PurchaseTopic string
	// Where to ask for document numbers and approvals.
	MasterdataAddr string
	ApprovalAddr   string
	// Live hints to open pages. Optional: without Redis the pages still work,
	// they just need a manual refresh.
	RedisAddr string
}

func Load() Config {
	return Config{
		DSN:                   env("DB_DSN", "postgres://erp_procurement:erp_procurement_pw@localhost:5433/erp_procurement?sslmode=disable"),
		GRPCPort:              env("GRPC_PORT", "9007"),
		KafkaBrokers:          strings.Split(env("KAFKA_BROKERS", "localhost:19092"), ","),
		StockTopic:            env("STOCK_TOPIC", "erp.inventory.stock.v1"),
		ConsumerGroup:         env("CONSUMER_GROUP", "procurement.stock.v1"),
		ApprovalTopic:         env("APPROVAL_TOPIC", "erp.approval.task.v1"),
		ApprovalConsumerGroup: env("APPROVAL_CONSUMER_GROUP", "procurement.approval.v1"),
		PurchaseTopic:         env("PURCHASE_TOPIC", "erp.procurement.purchase.v1"),
		MasterdataAddr:        env("MASTERDATA_ADDR", "localhost:9002"),
		ApprovalAddr:          env("APPROVAL_ADDR", "localhost:9005"),
		RedisAddr:             env("REDIS_ADDR", "localhost:6379"),
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

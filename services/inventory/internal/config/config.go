// Package config reads the service configuration from the environment.
package config

import (
	"os"
	"strings"
)

type Config struct {
	DSN      string
	GRPCPort string
	// masterdata issues outbound numbers; inventory does not mint its own.
	MasterdataAddr string
	KafkaBrokers   []string
	// Contract events come in here; stock allocation results go out on the
	// stock topic, which is what procurement listens to.
	ContractTopic string
	StockTopic    string
	ConsumerGroup string
	// Purchase receipts arrive here and become stock.
	PurchaseTopic         string
	PurchaseConsumerGroup string
}

func Load() Config {
	return Config{
		DSN:                   env("DB_DSN", "postgres://erp_inventory:erp_inventory_pw@localhost:5433/erp_inventory?sslmode=disable"),
		GRPCPort:              env("GRPC_PORT", "9008"),
		MasterdataAddr:        env("MASTERDATA_ADDR", "localhost:9002"),
		KafkaBrokers:          strings.Split(env("KAFKA_BROKERS", "localhost:19092"), ","),
		ContractTopic:         env("CONTRACT_TOPIC", "erp.export.contract.v1"),
		StockTopic:            env("STOCK_TOPIC", "erp.inventory.stock.v1"),
		ConsumerGroup:         env("CONSUMER_GROUP", "inventory.contract.v1"),
		PurchaseTopic:         env("PURCHASE_TOPIC", "erp.procurement.purchase.v1"),
		PurchaseConsumerGroup: env("PURCHASE_CONSUMER_GROUP", "inventory.purchase.v1"),
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

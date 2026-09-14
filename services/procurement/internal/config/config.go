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
	// Customer business is bought in full; there is no own-stock netting.
	ContractTopic string
	ConsumerGroup string
	// Approval decisions turn a submitted order into a placed one.
	ApprovalTopic         string
	ApprovalConsumerGroup string
	// Receipts leave here for inventory, which applies the stock increase.
	PurchaseTopic string
	// Where to ask for document numbers and approvals.
	MasterdataAddr string
	ApprovalAddr   string
	InventoryAddr  string
	FxAddr         string
	IAMAddr        string
	ExportAddr     string
	// Live hints to open pages. Optional: without Redis the pages still work,
	// they just need a manual refresh.
	RedisAddr string
	// Object storage for invoice scans. MinIO in dev, S3 in production —
	// same API, only the endpoint differs.
	MinioEndpoint       string
	MinioPublicEndpoint string
	MinioAccessKey      string
	MinioSecretKey      string
	MinioBucket         string
	MinioUseSSL         bool
}

func Load() Config {
	return Config{
		DSN:                   env("DB_DSN", "postgres://erp_procurement:erp_procurement_pw@localhost:5433/erp_procurement?sslmode=disable"),
		GRPCPort:              env("GRPC_PORT", "9007"),
		KafkaBrokers:          strings.Split(env("KAFKA_BROKERS", "localhost:19092"), ","),
		ContractTopic:         env("CONTRACT_TOPIC", "erp.export.contract.v1"),
		ConsumerGroup:         env("CONSUMER_GROUP", "procurement.contract.v1"),
		ApprovalTopic:         env("APPROVAL_TOPIC", "erp.approval.task.v1"),
		ApprovalConsumerGroup: env("APPROVAL_CONSUMER_GROUP", "procurement.approval.v1"),
		PurchaseTopic:         env("PURCHASE_TOPIC", "erp.procurement.purchase.v1"),
		MasterdataAddr:        env("MASTERDATA_ADDR", "localhost:9002"),
		ApprovalAddr:          env("APPROVAL_ADDR", "localhost:9005"),
		InventoryAddr:         env("INVENTORY_ADDR", "localhost:9008"),
		FxAddr:                env("FX_ADDR", "localhost:9003"),
		IAMAddr:               env("IAM_ADDR", "localhost:9001"),
		ExportAddr:            env("EXPORT_ADDR", "localhost:9006"),
		RedisAddr:             env("REDIS_ADDR", "localhost:6379"),
		MinioEndpoint:         env("MINIO_ENDPOINT", "localhost:19000"),
		MinioPublicEndpoint:   env("MINIO_PUBLIC_ENDPOINT", ""),
		MinioAccessKey:        env("MINIO_ACCESS_KEY", "erp"),
		MinioSecretKey:        env("MINIO_SECRET_KEY", "erp_dev_password"),
		MinioBucket:           env("MINIO_BUCKET", "erp-files"),
		MinioUseSSL:           env("MINIO_USE_SSL", "") == "true",
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

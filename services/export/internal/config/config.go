// Package config reads the service configuration from the environment.
package config

import (
	"os"
	"strings"
)

type Config struct {
	DSN            string
	GRPCPort       string
	MasterdataAddr string
	ProductAddr    string
	FxAddr         string
	ApprovalAddr   string
	IAMAddr        string
	// 那本唯一的银行流水账在采购服务手上（F2）。收款对账的每一次读写都要
	// 经过它，所以这个地址配错，收款对账整页打不开——不是少几行，是打不开。
	ProcurementAddr string
	KafkaBrokers   []string
	// Topic approval decisions arrive on, and the group export reads it as.
	ApprovalTopic string
	ConsumerGroup string
	// Topic this service's own events are published to.
	ContractTopic string
	// Inventory's topic, read for outbound events so a contract knows what
	// has shipped against it.
	StockTopic         string
	StockConsumerGroup string
	// Live hints to open pages.
	RedisAddr string
	// Object storage for contract paperwork. PublicEndpoint is where the
	// browser reaches it: SigV4 signs the host, so a presigned URL must be
	// signed for that address and not for the in-cluster one.
	MinioEndpoint       string
	MinioPublicEndpoint string
	MinioAccessKey      string
	MinioSecretKey      string
	MinioBucket         string
	MinioRegion         string
	MinioUseSSL         bool
	// Our own side of every contract. Configuration for now; it becomes
	// master data once there is more than one selling entity.
	SellerName    string
	SellerAddress string
}

func Load() Config {
	return Config{
		DSN:                 env("DB_DSN", "postgres://erp_export:erp_export_pw@localhost:5433/erp_export?sslmode=disable"),
		GRPCPort:            env("GRPC_PORT", "9006"),
		MasterdataAddr:      env("MASTERDATA_ADDR", "localhost:9002"),
		ProductAddr:         env("PRODUCT_ADDR", "localhost:9004"),
		FxAddr:              env("FX_ADDR", "localhost:9003"),
		ApprovalAddr:        env("APPROVAL_ADDR", "localhost:9005"),
		IAMAddr:             env("IAM_ADDR", "localhost:9001"),
		ProcurementAddr:     env("PROCUREMENT_ADDR", "localhost:9007"),
		KafkaBrokers:        strings.Split(env("KAFKA_BROKERS", "localhost:19092"), ","),
		ApprovalTopic:       env("APPROVAL_TOPIC", "erp.approval.task.v1"),
		ConsumerGroup:       env("CONSUMER_GROUP", "export.approval.v1"),
		ContractTopic:       env("CONTRACT_TOPIC", "erp.export.contract.v1"),
		StockTopic:          env("STOCK_TOPIC", "erp.inventory.stock.v1"),
		StockConsumerGroup:  env("STOCK_CONSUMER_GROUP", "export.stock.v1"),
		RedisAddr:           env("REDIS_ADDR", "localhost:6379"),
		MinioEndpoint:       env("MINIO_ENDPOINT", "localhost:19000"),
		MinioPublicEndpoint: env("MINIO_PUBLIC_ENDPOINT", ""),
		MinioAccessKey:      env("MINIO_ACCESS_KEY", "erp"),
		MinioSecretKey:      env("MINIO_SECRET_KEY", "erp_dev_password"),
		MinioBucket:         env("MINIO_BUCKET", "erp-files"),
		MinioRegion:         env("MINIO_REGION", "us-east-1"),
		MinioUseSSL:         env("MINIO_USE_SSL", "") == "true",
		SellerName:          env("SELLER_NAME", "宁波诺德进出口有限公司"),
		SellerAddress:       env("SELLER_ADDRESS", "浙江省宁波市鄞州区天童南路 999 号"),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

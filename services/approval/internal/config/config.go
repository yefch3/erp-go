// Package config reads the service configuration from the environment.
package config

import (
	"os"
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
	// PurchaseOrderFallbackRoleCode receives purchase approvals when the
	// submitter has no manager in the organisation tree.
	PurchaseOrderFallbackRoleCode string
}

func Load() Config {
	return Config{
		DSN:           env("DB_DSN", "postgres://erp_approval:erp_approval_pw@localhost:5433/erp_approval?sslmode=disable"),
		GRPCPort:      env("GRPC_PORT", "9005"),
		IAMAddr:       env("IAM_ADDR", "localhost:9001"),
		KafkaBrokers:  strings.Split(env("KAFKA_BROKERS", "localhost:19092"), ","),
		DecisionTopic: env("APPROVAL_TOPIC", "erp.approval.task.v1"),
		RedisAddr:     env("REDIS_ADDR", "localhost:6380"),
		// 角色**编码**，不是编号。角色编号是每家公司自己的：写死一个数字，
		// 等于让第二家公司去找一个属于第一家的角色，找不到就卡住，而提示还
		// 让他「配置采购审批角色成员」——那个角色在他公司里根本不存在。
		PurchaseOrderFallbackRoleCode: env("PURCHASE_ORDER_FALLBACK_ROLE_CODE", "PROCUREMENT_MANAGER"),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

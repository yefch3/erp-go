// Package config reads the service configuration from the environment.
// Defaults target local development against deploy/docker-compose.infra.yml.
package config

import (
	"os"
	"time"
)

type Config struct {
	DSN                  string
	GRPCPort             string
	JWTSecret            string
	JWTTTL               time.Duration
	AdminInitialPassword string
	// The first tenant, seeded on an empty database. The domains are what the
	// login page routes on, so getting them right matters more than the name.
	CompanyName        string
	CompanyMailDomains string
	AdminEmail         string

	// 再开一家公司（比如隔离测试用的）。四个都填才生效；引导键是第一个域名，
	// 开出来之后这几个变量就可以从 .env 里删掉。密码永远只进 .env，不进聊天
	// 不进代码。
	ExtraTenantName          string
	ExtraTenantMailDomains   string
	ExtraTenantAdminEmail    string
	ExtraTenantAdminPassword string

	// 员工头像的对象存储。iam 在这次之前不碰文件，所以这几项是**可选的**：
	// endpoint 留空就不接存储，员工资料照常读写，只是传不了头像。
	// 变量名和其他六个服务完全一致，运维那边不用为 iam 记一套新的。
	MinioEndpoint       string
	MinioPublicEndpoint string
	MinioAccessKey      string
	MinioSecretKey      string
	MinioBucket         string
	MinioUseSSL         bool
}

func Load() Config {
	return Config{
		DSN:       env("DB_DSN", "postgres://erp_iam:erp_iam_pw@localhost:5433/erp_iam?sslmode=disable"),
		GRPCPort:  env("GRPC_PORT", "9001"),
		JWTSecret: env("JWT_SECRET", "dev-secret-change-in-production"),
		// Twelve hours, and since the gateway renews on activity this is how
		// long somebody may sit *idle* — not how long since they signed in.
		// A working day never hits it; a machine left logged in at a
		// customer's office does, overnight.
		//
		// Twenty-four was the old value and meant something different: the
		// clock started at login and ran out mid-afternoon whether you were
		// working or not.
		JWTTTL: envDuration("JWT_TTL", 12*time.Hour),
		// Used only to bootstrap the very first account on an empty database.
		AdminInitialPassword: env("ADMIN_INITIAL_PASSWORD", "admin123"),
		CompanyName:          env("COMPANY_NAME", "Demo Company"),
		// Comma-separated: a Chinese exporter commonly holds both
		// example.com and example.com.cn, and both must reach one tenant.
		CompanyMailDomains: env("COMPANY_MAIL_DOMAINS", "example.com"),
		AdminEmail:         env("ADMIN_EMAIL", "admin@example.com"),

		// 默认全空 = 不开。刻意没有默认值：第二家公司只该在有人显式要它时出现。
		ExtraTenantName:          env("EXTRA_TENANT_NAME", ""),
		ExtraTenantMailDomains:   env("EXTRA_TENANT_MAIL_DOMAINS", ""),
		ExtraTenantAdminEmail:    env("EXTRA_TENANT_ADMIN_EMAIL", ""),
		ExtraTenantAdminPassword: env("EXTRA_TENANT_ADMIN_PASSWORD", ""),

		// 默认值和其他服务一致，本地 make up 起来就能用。生产的 endpoint 由
		// compose 传入；留空则不接存储（见 Config 上面的说明）。
		MinioEndpoint:       env("MINIO_ENDPOINT", "localhost:19000"),
		MinioPublicEndpoint: env("MINIO_PUBLIC_ENDPOINT", ""),
		MinioAccessKey:      env("MINIO_ACCESS_KEY", "erp"),
		MinioSecretKey:      env("MINIO_SECRET_KEY", "erp_dev_password"),
		MinioBucket:         env("MINIO_BUCKET", "erp-files"),
		MinioUseSSL:         env("MINIO_USE_SSL", "") == "true",
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
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

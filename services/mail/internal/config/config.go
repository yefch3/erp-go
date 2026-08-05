// Package config reads the service configuration from the environment.
package config

import (
	"os"
	"time"
)

type Config struct {
	DSN            string
	GRPCPort       string
	MasterdataAddr string
	IAMAddr        string
	// Provider selection. Only "dev" exists today: the sending service has
	// not been chosen and merchant onboarding has not happened, so nothing
	// can actually go out yet. Everything upstream is real.
	Provider string
	// Worker tuning. BatchSize doubles as the rate limit — providers cap
	// sends per second, and ignoring the cap is what produces the
	// "some succeeded, some failed" pattern this design exists to avoid.
	// Object storage for attachments and inline images.
	MinioEndpoint       string
	MinioPublicEndpoint string
	MinioAccessKey      string
	MinioSecretKey      string
	MinioBucket         string
	MinioUseSSL         bool

	// Base64 of a 32-byte key encrypting stored mailbox credentials. There is
	// no default and there must never be one: a built-in fallback produces a
	// system that looks encrypted and is not.
	CredKey        string
	CredKeyVersion int

	BatchSize      int32
	SendDelay      time.Duration
	DecisionWindow time.Duration
	// How long one SMTP conversation may take end to end. Mail hosts are
	// slower than APIs, and a greylisting server can sit on a connection for
	// a while before answering.
	SendTimeout time.Duration

	// Inbound polling. The interval is the honest latency of having no
	// webhook: a customer's reply appears within one cycle, not instantly.
	SyncInterval time.Duration
	SyncTimeout  time.Duration
	SyncBatch    int
	// How many mailboxes may be syncing at once. The scarce resource is not
	// our CPU — a goroutine waiting on a socket costs nothing — but the mail
	// host's patience and its connection allowances.
	SyncConcurrency int
	// Establishing a TCP connection is a different kind of wait from running
	// a command, and giving them one number means the sync waits a command's
	// worth of patience for a host that is simply not answering.
	DialTimeout time.Duration
	// Messages of history to hold per folder; the backfill stops here.
	SyncHistory int
	// Redis, for pushing "new mail" hints to open browser tabs.
	RedisAddr string
	// Google OAuth application credentials. The secret identifies our app to
	// Google, never a user to anything.
	GoogleClientID     string
	GoogleClientSecret string

	// Where a recipient's mail client can reach the gateway. Empty disables
	// open tracking: a pixel pointing at localhost would tell the recipient's
	// client to fetch from their own machine, which is worse than no pixel.
	PublicBaseURL string
}

func Load() Config {
	return Config{
		DSN:            env("DB_DSN", "postgres://erp_mail:erp_mail_pw@localhost:5433/erp_mail?sslmode=disable"),
		GRPCPort:       env("GRPC_PORT", "9010"),
		MasterdataAddr: env("MASTERDATA_ADDR", "localhost:9002"),
		IAMAddr:        env("IAM_ADDR", "localhost:9001"),
		Provider:       env("MAIL_PROVIDER", "dev"),
		MinioEndpoint:  env("MINIO_ENDPOINT", "localhost:19000"),
		// Browsers reach MinIO on the host port and SigV4 signs the host, so
		// presigned URLs must be signed for the address the browser will use.
		MinioPublicEndpoint: env("MINIO_PUBLIC_ENDPOINT", "localhost:19000"),
		MinioAccessKey:      env("MINIO_ACCESS_KEY", "erp"),
		MinioSecretKey:      env("MINIO_SECRET_KEY", "erp_dev_password"),
		MinioBucket:         env("MINIO_BUCKET", "erp-files"),
		MinioUseSSL:         os.Getenv("MINIO_USE_SSL") == "true",

		CredKey:        os.Getenv("MAIL_CRED_KEY"),
		CredKeyVersion: envInt("MAIL_CRED_KEY_VERSION", 1),

		BatchSize:          int32(envInt("MAIL_BATCH_SIZE", 20)),
		SendDelay:          envDuration("MAIL_SEND_DELAY", 100*time.Millisecond),
		DecisionWindow:     envDuration("MAIL_DECISION_WINDOW", 10*time.Minute),
		SendTimeout:        envDuration("MAIL_SEND_TIMEOUT", 45*time.Second),
		SyncInterval:       envDuration("MAIL_SYNC_INTERVAL", 2*time.Minute),
		SyncTimeout:        envDuration("MAIL_SYNC_TIMEOUT", 90*time.Second),
		SyncBatch:          envInt("MAIL_SYNC_BATCH", 50),
		SyncConcurrency:    envInt("MAIL_SYNC_CONCURRENCY", 8),
		DialTimeout:        envDuration("MAIL_DIAL_TIMEOUT", 10*time.Second),
		SyncHistory:        envInt("MAIL_SYNC_HISTORY", 500),
		RedisAddr:          os.Getenv("REDIS_ADDR"),
		GoogleClientID:     os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"),
		PublicBaseURL:      os.Getenv("MAIL_PUBLIC_BASE_URL"),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		n := 0
		for _, r := range v {
			if r < '0' || r > '9' {
				return def
			}
			n = n*10 + int(r-'0')
		}
		if n > 0 {
			return n
		}
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

package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/iam/internal/adapter/grpcin"
	"github.com/sgao19/erp-go/services/iam/internal/app"
	"github.com/sgao19/erp-go/services/iam/internal/config"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "iam")
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgdb.New(ctx, cfg.DSN)
	if err != nil {
		return err
	}
	defer pool.Close()

	svc := app.New(pool, cfg.JWTSecret, cfg.JWTTTL, log)
	if err := svc.EnsureAdmin(ctx, 1, app.SeedTenant{
		CompanyName:     cfg.CompanyName,
		MailDomains:     cfg.CompanyMailDomains,
		AdminEmail:      cfg.AdminEmail,
		InitialPassword: cfg.AdminInitialPassword,
	}); err != nil {
		return err
	}
	// 第二家公司（有配置才开；幂等键是第一个域名，见 EnsureExtraTenant）。
	// 四个变量填了任何一个就全都要填——填一半是配置错误，这里让启动失败，
	// 因为改配置的人此刻正看着启动日志。
	if cfg.ExtraTenantName != "" || cfg.ExtraTenantMailDomains != "" ||
		cfg.ExtraTenantAdminEmail != "" || cfg.ExtraTenantAdminPassword != "" {
		if err := svc.EnsureExtraTenant(ctx, app.SeedTenant{
			CompanyName:     cfg.ExtraTenantName,
			MailDomains:     cfg.ExtraTenantMailDomains,
			AdminEmail:      cfg.ExtraTenantAdminEmail,
			InitialPassword: cfg.ExtraTenantAdminPassword,
		}); err != nil {
			return err
		}
	}
	// 给此改动之前开出的公司补齐预置角色（新公司在 seedTenantCore 里当场播）。
	// 启动失败而不是记条日志：预置角色播不进去意味着库或权限表有问题，带病
	// 启动只会把症状推迟到某家公司提交采购单的那一刻。
	if err := svc.EnsurePresetRoles(ctx); err != nil {
		return err
	}

	srv := grpc.NewServer(grpcx.ServerInterceptors(log))
	h := grpcin.New(svc)
	iamv1.RegisterAuthServiceServer(srv, h)
	iamv1.RegisterDirectoryServiceServer(srv, h)
	iamv1.RegisterAccessServiceServer(srv, h)
	iamv1.RegisterPlatformServiceServer(srv, h)
	reflection.Register(srv) // grpcurl-friendly during development

	lis, err := net.Listen("tcp", ":"+cfg.GRPCPort)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		log.Info("shutting down")
		srv.GracefulStop()
	}()
	log.Info("iam listening", "port", cfg.GRPCPort)
	return srv.Serve(lis)
}

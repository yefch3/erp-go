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
	if err := svc.EnsureAdmin(ctx, 1, cfg.AdminInitialPassword); err != nil {
		return err
	}

	srv := grpc.NewServer(grpcx.ServerInterceptors(log))
	h := grpcin.New(svc)
	iamv1.RegisterAuthServiceServer(srv, h)
	iamv1.RegisterDirectoryServiceServer(srv, h)
	iamv1.RegisterAccessServiceServer(srv, h)
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

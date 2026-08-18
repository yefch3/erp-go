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

	fxv1 "github.com/sgao19/erp-go/gen/go/erp/fx/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/fx/internal/adapter/grpcin"
	"github.com/sgao19/erp-go/services/fx/internal/app"
	"github.com/sgao19/erp-go/services/fx/internal/config"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "fx")
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

	svc := app.New(pool, log)
	go svc.RunFetcher(ctx, cfg.FetchURL, cfg.FetchSymbols, cfg.FetchInterval, cfg.BackfillDays)

	srv := grpc.NewServer(grpcx.ServerInterceptors(log))
	fxv1.RegisterFxServiceServer(srv, grpcin.New(svc))
	reflection.Register(srv)

	lis, err := net.Listen("tcp", ":"+cfg.GRPCPort)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		log.Info("shutting down")
		srv.GracefulStop()
	}()
	log.Info("fx listening", "port", cfg.GRPCPort)
	return srv.Serve(lis)
}

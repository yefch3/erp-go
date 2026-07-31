package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	pdv1 "github.com/sgao19/erp-go/gen/go/erp/product/v1"
	"github.com/sgao19/erp-go/pkg/blobstore"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/product/internal/adapter/grpcin"
	"github.com/sgao19/erp-go/services/product/internal/adapter/grpcout"
	"github.com/sgao19/erp-go/services/product/internal/app"
	"github.com/sgao19/erp-go/services/product/internal/config"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "product")
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

	mdConn, err := grpc.NewClient(cfg.MasterdataAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(grpcx.UnaryClientPropagator()))
	if err != nil {
		return err
	}
	defer mdConn.Close()

	files, err := blobstore.New(ctx, blobstore.Config{
		Endpoint:       cfg.MinioEndpoint,
		PublicEndpoint: cfg.MinioPublicEndpoint,
		AccessKey:      cfg.MinioAccessKey,
		SecretKey:      cfg.MinioSecretKey,
		Bucket:         cfg.MinioBucket,
		UseSSL:         cfg.MinioUseSSL,
	})
	if err != nil {
		return err
	}

	svc := app.New(pool, grpcout.NewNumbering(mdConn), grpcout.NewFiles(files))
	srv := grpc.NewServer(grpcx.ServerInterceptors(log))
	h := grpcin.New(svc)
	pdv1.RegisterCatalogServiceServer(srv, h)
	pdv1.RegisterAttachmentServiceServer(srv, h)
	pdv1.RegisterAttributeServiceServer(srv, h)
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
	log.Info("product listening", "port", cfg.GRPCPort, "bucket", cfg.MinioBucket)
	return srv.Serve(lis)
}

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

	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"github.com/sgao19/erp-go/pkg/blobstore"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/livefeed"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/shipping/internal/adapter/grpcin"
	"github.com/sgao19/erp-go/services/shipping/internal/adapter/grpcout"
	"github.com/sgao19/erp-go/services/shipping/internal/app"
	"github.com/sgao19/erp-go/services/shipping/internal/config"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "shipping")
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func dial(addr string) (*grpc.ClientConn, error) {
	return grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(grpcx.UnaryClientPropagator()))
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

	iamConn, err := dial(cfg.IAMAddr)
	if err != nil {
		return err
	}
	defer iamConn.Close()
	masterdataConn, err := dial(cfg.MasterdataAddr)
	if err != nil {
		return err
	}
	defer masterdataConn.Close()

	files, err := blobstore.New(ctx, blobstore.Config{
		Endpoint: cfg.MinioEndpoint, PublicEndpoint: cfg.MinioPublicEndpoint,
		AccessKey: cfg.MinioAccessKey, SecretKey: cfg.MinioSecretKey,
		Bucket: cfg.MinioBucket, UseSSL: cfg.MinioUseSSL,
		// See the note in mail's main.go: S3 needs the region or it 301s.
		Region: os.Getenv("MINIO_REGION"),
	})
	if err != nil {
		return err
	}

	svc := app.New(pool, grpcout.NewFiles(files))
	svc.UseAccessControl(grpcout.NewScopes(iamConn), grpcout.NewCustomerAccess(masterdataConn))
	svc.UseLogger(log)
	if cfg.RedisAddr != "" {
		publisher := livefeed.NewPublisher(cfg.RedisAddr, log)
		defer publisher.Close()
		svc.UseReminderNotifier(grpcout.NewReminderNotifier(publisher))
	}
	go svc.RunArrivalReminderWorker(ctx, cfg.ReminderInterval, cfg.ReminderBatchSize)

	srv := grpc.NewServer(grpcx.ServerInterceptors(log))
	shippingv1.RegisterShippingServiceServer(srv, grpcin.New(svc))
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
	log.Info("shipping listening", "port", cfg.GRPCPort)
	return srv.Serve(lis)
}

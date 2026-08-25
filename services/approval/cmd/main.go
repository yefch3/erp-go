package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	apv1 "github.com/sgao19/erp-go/gen/go/erp/approval/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/kafkax"
	"github.com/sgao19/erp-go/pkg/livefeed"
	"github.com/sgao19/erp-go/pkg/outbox"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/approval/internal/adapter/grpcin"
	"github.com/sgao19/erp-go/services/approval/internal/adapter/grpcout"
	"github.com/sgao19/erp-go/services/approval/internal/app"
	"github.com/sgao19/erp-go/services/approval/internal/config"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "approval")
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

	iamConn, err := grpc.NewClient(cfg.IAMAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(grpcx.UnaryClientPropagator()))
	if err != nil {
		return err
	}
	defer iamConn.Close()

	// Live hints to whoever has a page open. Separate from Kafka on purpose:
	// see pkg/livefeed.
	live := livefeed.NewPublisher(cfg.RedisAddr, log)
	defer live.Close()

	svc := app.New(pool, grpcout.NewIAM(iamConn), live, cfg.PurchaseOrderFallbackRoleID)

	// The relay is what turns committed outbox rows into Kafka messages;
	// business code never touches the producer.
	producer := kafkax.NewProducer(cfg.KafkaBrokers)
	defer producer.Close()
	relay := outbox.NewRelay(pool, producer, func(string) string { return cfg.DecisionTopic }, log)
	go func() {
		if err := relay.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("outbox relay stopped", "err", err)
		}
	}()

	srv := grpc.NewServer(grpcx.ServerInterceptors(log))
	apv1.RegisterApprovalServiceServer(srv, grpcin.New(svc))
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
	log.Info("approval listening", "port", cfg.GRPCPort, "topic", cfg.DecisionTopic)
	return srv.Serve(lis)
}

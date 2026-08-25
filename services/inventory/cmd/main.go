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

	ivv1 "github.com/sgao19/erp-go/gen/go/erp/inventory/v1"
	"github.com/sgao19/erp-go/pkg/deadletter"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/idempotency"
	"github.com/sgao19/erp-go/pkg/kafkax"
	"github.com/sgao19/erp-go/pkg/outbox"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/inventory/internal/adapter/grpcin"
	"github.com/sgao19/erp-go/services/inventory/internal/adapter/grpcout"
	"github.com/sgao19/erp-go/services/inventory/internal/adapter/kafkain"
	"github.com/sgao19/erp-go/services/inventory/internal/app"
	"github.com/sgao19/erp-go/services/inventory/internal/config"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "inventory")
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

	svc := app.New(pool, grpcout.NewNumbering(mdConn))

	// Outbound: allocation results become StockAllocated messages, which is
	// what procurement acts on. Business code never touches the producer.
	producer := kafkax.NewProducer(cfg.KafkaBrokers)
	defer producer.Close()
	relay := outbox.NewRelay(pool, producer, func(string) string { return cfg.StockTopic }, log)
	go func() {
		if err := relay.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("outbox relay stopped", "err", err)
		}
	}()

	// Inbound: contracts that took effect claim stock here first.
	contracts := kafkax.NewConsumer(cfg.KafkaBrokers, cfg.ConsumerGroup, cfg.ContractTopic,
		idempotency.New(pool, cfg.ConsumerGroup), deadletter.New(pool, cfg.ConsumerGroup), kafkain.ContractEvents(svc, log), log)
	go func() {
		if err := contracts.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("contract consumer stopped", "err", err)
		}
	}()

	// Deliveries booked by procurement become stock here, which in turn hands
	// them to whichever contracts were waiting.
	purchases := kafkax.NewConsumer(cfg.KafkaBrokers, cfg.PurchaseConsumerGroup, cfg.PurchaseTopic,
		idempotency.New(pool, cfg.PurchaseConsumerGroup), deadletter.New(pool, cfg.PurchaseConsumerGroup), kafkain.PurchaseEvents(svc, log), log)
	go func() {
		if err := purchases.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("purchase consumer stopped", "err", err)
		}
	}()

	srv := grpc.NewServer(grpcx.ServerInterceptors(log))
	ivv1.RegisterStockServiceServer(srv, grpcin.New(svc))
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
	log.Info("inventory listening", "port", cfg.GRPCPort,
		"consumes", cfg.ContractTopic, "produces", cfg.StockTopic)
	return srv.Serve(lis)
}

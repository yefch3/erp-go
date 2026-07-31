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

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/idempotency"
	"github.com/sgao19/erp-go/pkg/kafkax"
	"github.com/sgao19/erp-go/pkg/livefeed"
	"github.com/sgao19/erp-go/pkg/outbox"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/adapter/grpcin"
	"github.com/sgao19/erp-go/services/procurement/internal/adapter/grpcout"
	"github.com/sgao19/erp-go/services/procurement/internal/adapter/kafkain"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
	"github.com/sgao19/erp-go/services/procurement/internal/config"
)

func dial(addr string) (*grpc.ClientConn, error) {
	return grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(grpcx.UnaryClientPropagator()))
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "procurement")
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

	live := livefeed.NewPublisher(cfg.RedisAddr, log)
	defer live.Close()

	mdConn, err := dial(cfg.MasterdataAddr)
	if err != nil {
		return err
	}
	defer mdConn.Close()
	apConn, err := dial(cfg.ApprovalAddr)
	if err != nil {
		return err
	}
	defer apConn.Close()

	svc := app.New(pool, app.Deps{
		Numbering: grpcout.NewNumbering(mdConn),
		Approvals: grpcout.NewApprovals(apConn),
		Live:      live,
	})

	// Receipts go out as events: the stock increase and the receipt record
	// must either both happen or neither, and only the outbox can promise
	// that across two databases.
	producer := kafkax.NewProducer(cfg.KafkaBrokers)
	defer producer.Close()
	relay := outbox.NewRelay(pool, producer, func(string) string { return cfg.PurchaseTopic }, log)
	go func() {
		if err := relay.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("outbox relay stopped", "err", err)
		}
	}()

	decisions := kafkax.NewConsumer(cfg.KafkaBrokers, cfg.ApprovalConsumerGroup, cfg.ApprovalTopic,
		idempotency.New(pool, cfg.ApprovalConsumerGroup), kafkain.ApprovalDecisions(svc, log), log)
	go func() {
		if err := decisions.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("approval consumer stopped", "err", err)
		}
	}()

	// Purely a consumer: requirements are not created through the API, they
	// are what inventory says stock could not cover.
	shortages := kafkax.NewConsumer(cfg.KafkaBrokers, cfg.ConsumerGroup, cfg.StockTopic,
		idempotency.New(pool, cfg.ConsumerGroup), kafkain.StockEvents(svc, log), log)
	go func() {
		if err := shortages.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("stock consumer stopped", "err", err)
		}
	}()

	srv := grpc.NewServer(grpcx.ServerInterceptors(log))
	prv1.RegisterRequirementServiceServer(srv, grpcin.New(svc))
	prv1.RegisterPurchaseOrderServiceServer(srv, grpcin.NewOrders(svc))
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
	log.Info("procurement listening", "port", cfg.GRPCPort, "consumes", cfg.StockTopic)
	return srv.Serve(lis)
}

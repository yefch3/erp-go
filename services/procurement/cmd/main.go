package main

import (
	"context"
	"errors"
	"github.com/shopspring/decimal"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/blobstore"
	"github.com/sgao19/erp-go/pkg/deadletter"
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
	invConn, err := dial(cfg.InventoryAddr)
	if err != nil {
		return err
	}
	defer invConn.Close()
	fxConn, err := dial(cfg.FxAddr)
	if err != nil {
		return err
	}
	defer fxConn.Close()
	iamConn, err := dial(cfg.IAMAddr)
	if err != nil {
		return err
	}
	defer iamConn.Close()

	files, err := blobstore.New(ctx, blobstore.Config{
		Endpoint:       cfg.MinioEndpoint,
		PublicEndpoint: cfg.MinioPublicEndpoint,
		AccessKey:      cfg.MinioAccessKey,
		SecretKey:      cfg.MinioSecretKey,
		Bucket:         cfg.MinioBucket,
		UseSSL:         cfg.MinioUseSSL,
		// See the note in mail's main.go: S3 needs the region or it 301s.
		Region: os.Getenv("MINIO_REGION"),
	})
	if err != nil {
		return err
	}

	svc := app.New(pool, app.Deps{
		Log:        log,
		Numbering:  grpcout.NewNumbering(mdConn),
		Approvals:  grpcout.NewApprovals(apConn),
		Suppliers:  grpcout.NewSuppliers(mdConn),
		Warehouses: grpcout.NewWarehouses(invConn),
		Rates:      grpcout.NewRates(fxConn),
		Scopes:     grpcout.NewScopes(iamConn),
		Files:      grpcout.NewFiles(files),
		Live:       live,
	})
	// Three-way-match tolerance. Zero unless set: a pilot should first see
	// how often reality differs before deciding how much to stop looking at.
	//   MATCH_TOLERANCE_PCT=0.01  → 1% of the payable amount
	//   MATCH_TOLERANCE_ABS=5     → 5 currency units
	// The line tolerance is min() of whichever are set.
	tolPct, _ := decimal.NewFromString(os.Getenv("MATCH_TOLERANCE_PCT"))
	tolAbs, _ := decimal.NewFromString(os.Getenv("MATCH_TOLERANCE_ABS"))
	svc.UseMatchTolerance(tolPct, tolAbs)
	// Book currency for fx snapshots; empty keeps CNY (see fxsnapshot.go).
	svc.UseBaseCurrency(os.Getenv("BASE_CURRENCY"))

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

	decisionHandler := kafkain.ApprovalDecisions(svc, log)
	dlDecision := deadletter.New(pool, cfg.ApprovalConsumerGroup)
	decisions := kafkax.NewConsumer(cfg.KafkaBrokers, cfg.ApprovalConsumerGroup, cfg.ApprovalTopic,
		idempotency.New(pool, cfg.ApprovalConsumerGroup), dlDecision, decisionHandler, log)
	go func() {
		if err := decisions.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("approval consumer stopped", "err", err)
		}
	}()

	// Every effective contract line becomes a full purchase requirement. This
	// company has no own stock pool to net before ordering from the mill.
	contractHandler := kafkain.ContractEvents(svc, log)
	dlContract := deadletter.New(pool, cfg.ConsumerGroup)
	contracts := kafkax.NewConsumer(cfg.KafkaBrokers, cfg.ConsumerGroup, cfg.ContractTopic,
		idempotency.New(pool, cfg.ConsumerGroup), dlContract, contractHandler, log)
	go func() {
		if err := contracts.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("contract consumer stopped", "err", err)
		}
	}()

	srv := grpc.NewServer(grpcx.ServerInterceptors(log))
	prv1.RegisterRequirementServiceServer(srv, grpcin.New(svc))
	console := deadletter.NewConsole()
	console.Add(cfg.ApprovalConsumerGroup, dlDecision, kafkax.ReplayHandler(decisionHandler, idempotency.New(pool, cfg.ApprovalConsumerGroup)))
	console.Add(cfg.ConsumerGroup, dlContract, kafkax.ReplayHandler(contractHandler, idempotency.New(pool, cfg.ConsumerGroup)))
	orders := grpcin.NewOrders(svc)
	orders.UseDeadLetterConsole(console)
	prv1.RegisterPurchaseOrderServiceServer(srv, orders)
	prv1.RegisterSourcingServiceServer(srv, grpcin.NewSourcing(svc))
	prv1.RegisterInquiryTemplateServiceServer(srv, grpcin.NewInquiryTemplates(svc))
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
	log.Info("procurement listening", "port", cfg.GRPCPort, "consumes", cfg.ContractTopic)
	return srv.Serve(lis)
}

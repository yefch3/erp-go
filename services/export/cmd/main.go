package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	"github.com/sgao19/erp-go/pkg/blobstore"
	"github.com/sgao19/erp-go/pkg/deadletter"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/idempotency"
	"github.com/sgao19/erp-go/pkg/kafkax"
	"github.com/sgao19/erp-go/pkg/livefeed"
	"github.com/sgao19/erp-go/pkg/outbox"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/export/internal/adapter/grpcin"
	"github.com/sgao19/erp-go/services/export/internal/adapter/grpcout"
	"github.com/sgao19/erp-go/services/export/internal/adapter/kafkain"
	"github.com/sgao19/erp-go/services/export/internal/app"
	"github.com/sgao19/erp-go/services/export/internal/config"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "export")
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

	mdConn, err := dial(cfg.MasterdataAddr)
	if err != nil {
		return err
	}
	defer mdConn.Close()
	pdConn, err := dial(cfg.ProductAddr)
	if err != nil {
		return err
	}
	defer pdConn.Close()
	fxConn, err := dial(cfg.FxAddr)
	if err != nil {
		return err
	}
	defer fxConn.Close()
	apConn, err := dial(cfg.ApprovalAddr)
	if err != nil {
		return err
	}
	defer apConn.Close()
	iamConn, err := dial(cfg.IAMAddr)
	if err != nil {
		return err
	}
	defer iamConn.Close()

	// Contract paperwork goes straight from the browser to object storage;
	// this service only signs the URLs and records what landed.
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

	// One client, two roles: it submits documents and it answers "what am I
	// being asked to approve".
	live := livefeed.NewPublisher(cfg.RedisAddr, log)
	defer live.Close()

	approvals := grpcout.NewApprovals(apConn)

	svc := app.New(pool, app.Deps{
		Customers:   grpcout.NewCustomers(mdConn),
		Products:    grpcout.NewProducts(pdConn),
		Rates:       grpcout.NewRates(fxConn),
		Numbering:   grpcout.NewNumbering(mdConn),
		Approvals:   approvals,
		Involvement: approvals,
		Files:       grpcout.NewFiles(files),
		Scopes:      grpcout.NewScopes(iamConn),
		Live:        live,
		Directory:   grpcout.NewDirectory(iamConn),
		Seller:      app.Seller{Name: cfg.SellerName, Address: cfg.SellerAddress},
	})

	// Outbound: committed outbox rows become Kafka messages. Business code
	// never touches the producer.
	producer := kafkax.NewProducer(cfg.KafkaBrokers)
	defer producer.Close()
	relay := outbox.NewRelay(pool, producer, func(string) string { return cfg.ContractTopic }, log)
	go func() {
		if err := relay.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("outbox relay stopped", "err", err)
		}
	}()

	// Inbound: approval decisions. Dedupe is keyed by consumer group, so a
	// second consumer added here later cannot swallow this one's events.
	decisions := kafkax.NewConsumer(cfg.KafkaBrokers, cfg.ConsumerGroup, cfg.ApprovalTopic,
		idempotency.New(pool, cfg.ConsumerGroup), deadletter.New(pool, cfg.ConsumerGroup), kafkain.ApprovalDecisions(svc, log), log)
	go func() {
		if err := decisions.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("approval consumer stopped", "err", err)
		}
	}()

	// Inbound: what the warehouse shipped. Its own consumer group, so it
	// cannot swallow the approval consumer's events or be swallowed by them.
	shipments := kafkax.NewConsumer(cfg.KafkaBrokers, cfg.StockConsumerGroup, cfg.StockTopic,
		idempotency.New(pool, cfg.StockConsumerGroup), deadletter.New(pool, cfg.StockConsumerGroup), kafkain.StockEvents(svc, log), log)
	go func() {
		if err := shipments.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("stock consumer stopped", "err", err)
		}
	}()

	// 应收到期提醒（E1）：每天扫一趟，把该提醒而未提醒的写成站内信。
	// 跨租户，启动时先跑一次补上停机期间的。
	go svc.RunReceivableReminderWorker(ctx, 24*time.Hour, log)

	srv := grpc.NewServer(grpcx.ServerInterceptors(log))
	exv1.RegisterQuotationServiceServer(srv, grpcin.New(svc))
	exv1.RegisterContractServiceServer(srv, grpcin.NewContracts(svc))
	exv1.RegisterShipmentServiceServer(srv, grpcin.NewShipments(svc))
	exv1.RegisterReceiptServiceServer(srv, grpcin.NewReceipts(svc))
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
	log.Info("export listening", "port", cfg.GRPCPort,
		"consumes", cfg.ApprovalTopic, "produces", cfg.ContractTopic)
	return srv.Serve(lis)
}

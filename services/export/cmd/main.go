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

	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	"github.com/sgao19/erp-go/pkg/blobstore"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/idempotency"
	"github.com/sgao19/erp-go/pkg/kafkax"
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
	})
	if err != nil {
		return err
	}

	// One client, two roles: it submits documents and it answers "what am I
	// being asked to approve".
	approvals := grpcout.NewApprovals(apConn)

	svc := app.New(pool,
		grpcout.NewCustomers(mdConn),
		grpcout.NewProducts(pdConn),
		grpcout.NewRates(fxConn),
		grpcout.NewNumbering(mdConn),
		approvals,
		grpcout.NewFiles(files),
		grpcout.NewScopes(iamConn),
		approvals,
		app.Seller{Name: cfg.SellerName, Address: cfg.SellerAddress},
	)

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
		idempotency.New(pool, cfg.ConsumerGroup), kafkain.ApprovalDecisions(svc, log), log)
	go func() {
		if err := decisions.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("approval consumer stopped", "err", err)
		}
	}()

	srv := grpc.NewServer(grpcx.ServerInterceptors(log))
	exv1.RegisterQuotationServiceServer(srv, grpcin.New(svc))
	exv1.RegisterContractServiceServer(srv, grpcin.NewContracts(svc))
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

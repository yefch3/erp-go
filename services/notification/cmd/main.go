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

	ntv1 "github.com/sgao19/erp-go/gen/go/erp/notification/v1"
	"github.com/sgao19/erp-go/pkg/blobstore"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/notification/internal/adapter/grpcin"
	"github.com/sgao19/erp-go/services/notification/internal/adapter/grpcout"
	"github.com/sgao19/erp-go/services/notification/internal/adapter/provider"
	"github.com/sgao19/erp-go/services/notification/internal/app"
	"github.com/sgao19/erp-go/services/notification/internal/config"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "notification")
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

	mdConn, err := dial(cfg.MasterdataAddr)
	if err != nil {
		return err
	}
	defer mdConn.Close()
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
	})
	if err != nil {
		return err
	}

	// Which provider actually puts mail on the wire is configuration, not
	// code: the sending service has not been chosen yet, and everything
	// upstream of this line is finished and testable without one.
	sender := provider.Pick(cfg.Provider, log)

	svc := app.New(pool, app.Deps{
		Numbering: grpcout.NewNumbering(mdConn),
		Directory: grpcout.NewDirectory(iamConn),
		Scopes:    grpcout.NewScopes(iamConn),
		Provider:  sender,
		Files:     grpcout.NewFiles(files),
	}, log)

	// The worker runs in-process. The database is the queue, so a second
	// replica would simply claim different rows - SKIP LOCKED makes that
	// safe without any coordination.
	go svc.RunWorker(ctx, app.WorkerConfig{
		BatchSize:      cfg.BatchSize,
		SendDelay:      cfg.SendDelay,
		DecisionWindow: cfg.DecisionWindow,
	})

	srv := grpc.NewServer(grpcx.ServerInterceptors(log))
	ntv1.RegisterEmailServiceServer(srv, grpcin.New(svc))
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
	log.Info("notification listening", "port", cfg.GRPCPort, "provider", sender.Name())
	return srv.Serve(lis)
}

func dial(addr string) (*grpc.ClientConn, error) {
	return grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(grpcx.UnaryClientPropagator()))
}

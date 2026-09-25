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

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/blobstore"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/masterdata/internal/adapter/filestore"
	"github.com/sgao19/erp-go/services/masterdata/internal/adapter/grpcin"
	"github.com/sgao19/erp-go/services/masterdata/internal/app"
	"github.com/sgao19/erp-go/services/masterdata/internal/config"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "masterdata")
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

	svc := app.New(pool)
	svc.UseFiles(filestore.New(files))
	check, closeReferences, err := referenceChecker()
	if err != nil {
		return err
	}
	defer closeReferences()
	svc.UseReferenceChecker(check)
	iamConn, err := grpc.NewClient(cfg.IAMAddr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(grpcx.UnaryClientPropagator()))
	if err != nil {
		return err
	}
	defer iamConn.Close()
	h := grpcin.New(svc)
	accessClient := iamv1.NewAccessServiceClient(iamConn)
	srv := grpc.NewServer(grpcx.ServerInterceptors(log), grpc.MaxRecvMsgSize(12<<20), grpc.MaxSendMsgSize(12<<20), grpc.ChainUnaryInterceptor(h.CustomerAccess(accessClient), h.SupplierAccess(accessClient)))
	mdv1.RegisterCustomerServiceServer(srv, h)
	mdv1.RegisterMasterDataBulkDeleteServiceServer(srv, &grpcin.BulkDeleteHandler{Service: svc, Access: accessClient})
	mdv1.RegisterSupplierServiceServer(srv, h)
	mdv1.RegisterPortServiceServer(srv, h)
	mdv1.RegisterOptionServiceServer(srv, h)
	mdv1.RegisterNumberingServiceServer(srv, h)
	mdv1.RegisterCreditRatingServiceServer(srv, h)
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
	log.Info("masterdata listening", "port", cfg.GRPCPort)
	return srv.Serve(lis)
}

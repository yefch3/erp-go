package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	apv1 "github.com/sgao19/erp-go/gen/go/erp/approval/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	fxv1 "github.com/sgao19/erp-go/gen/go/erp/fx/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/gateway/internal/config"
	"github.com/sgao19/erp-go/services/gateway/internal/httpapi"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "gateway")
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func dial(addr string) (*grpc.ClientConn, error) {
	return grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(grpcx.UnaryClientPropagator()),
	)
}

func run(log *slog.Logger) error {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	iamConn, err := dial(cfg.IAMAddr)
	if err != nil {
		return err
	}
	defer iamConn.Close()
	mdConn, err := dial(cfg.MasterdataAddr)
	if err != nil {
		return err
	}
	defer mdConn.Close()
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

	srv := &httpapi.Server{
		IAM:       iamv1.NewAuthServiceClient(iamConn),
		Access:    iamv1.NewAccessServiceClient(iamConn),
		Fx:        fxv1.NewFxServiceClient(fxConn),
		Customers: mdv1.NewCustomerServiceClient(mdConn),
		Suppliers: mdv1.NewSupplierServiceClient(mdConn),
		Options:   mdv1.NewOptionServiceClient(mdConn),
		Numbering: mdv1.NewNumberingServiceClient(mdConn),
		Approval:  apv1.NewApprovalServiceClient(apConn),
		JWTSecret: cfg.JWTSecret,
		Log:       log,
	}

	httpSrv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()
	log.Info("gateway listening", "port", cfg.HTTPPort)
	if err := httpSrv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

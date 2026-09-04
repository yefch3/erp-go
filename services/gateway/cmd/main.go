package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	apv1 "github.com/sgao19/erp-go/gen/go/erp/approval/v1"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	fxv1 "github.com/sgao19/erp-go/gen/go/erp/fx/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	ivv1 "github.com/sgao19/erp-go/gen/go/erp/inventory/v1"
	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	pdv1 "github.com/sgao19/erp-go/gen/go/erp/product/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/livefeed"
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

// maxServiceResponseBytes 是网关愿意从内部服务收下的单条响应上限。
//
// gRPC 默认只肯收 4 MB，而**附件打包下载是整个包一条消息传过来的**
// （app.MaxZipBytes = 40 MB）。默认值下，一封带 5 MB 附件的信点「下载全部」
// 会在这里失败，而且错误消息是传输层的，跟附件、跟大小都不沾边——
// 「grpc: received message larger than max」，看不出该去改哪个数。
//
// 留了余量装 zip 的头尾和 protobuf 的封装。改 MaxZipBytes 就要跟着改这里。
const maxServiceResponseBytes = 48 << 20

func dial(addr string) (*grpc.ClientConn, error) {
	return grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(grpcx.UnaryClientPropagator()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxServiceResponseBytes)),
	)
}

func run(log *slog.Logger) error {
	// Read here so a missing key kills the gateway on start with one readable
	// line. It is only used on outgoing calls, so without this the first
	// symptom would be a panic in the middle of somebody's first request.
	_ = grpcx.SigningKey()
	cfg := config.Load()
	procurementSenderID, _ := strconv.ParseInt(os.Getenv("PROCUREMENT_MAIL_SENDER_ID"), 10, 64)
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
	pdConn, err := dial(cfg.ProductAddr)
	if err != nil {
		return err
	}
	defer pdConn.Close()
	exConn, err := dial(cfg.ExportAddr)
	if err != nil {
		return err
	}
	defer exConn.Close()
	prConn, err := dial(cfg.ProcurementAddr)
	if err != nil {
		return err
	}
	defer prConn.Close()
	ivConn, err := dial(cfg.InventoryAddr)
	if err != nil {
		return err
	}
	defer ivConn.Close()
	shippingConn, err := dial(cfg.ShippingAddr)
	if err != nil {
		return err
	}
	defer shippingConn.Close()
	ntConn, err := dial(cfg.MailAddr)
	if err != nil {
		return err
	}
	defer ntConn.Close()

	// The live feed is a hint channel, not the event bus, but a gateway that
	// silently serves a stream nobody ever publishes into is worse than one
	// that refuses to start.
	live := livefeed.NewSubscriber(cfg.RedisAddr)
	defer live.Close()
	if err := live.Ping(ctx); err != nil {
		return fmt.Errorf("gateway: redis at %s: %w", cfg.RedisAddr, err)
	}

	// Mailbox unlock tokens. Twelve hours by default: verify once in the
	// morning, locked again by the next working day.
	unlockTTL := 12 * time.Hour
	if v := os.Getenv("MAIL_UNLOCK_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			unlockTTL = d
		}
	}
	unlock := httpapi.NewUnlockStore(cfg.RedisAddr, unlockTTL)
	// Ends one person's sessions without ending everybody's. Reads come from
	// an in-memory snapshot this loop keeps fresh, so an unreachable Redis
	// keeps enforcing what was last known rather than signing out the company
	// or quietly un-revoking somebody. See RevocationStore.
	revocations := httpapi.NewRevocationStore(cfg.RedisAddr, log)
	defer revocations.Close()
	go revocations.Run(ctx)

	// Failed-attempt budgets for login and mailbox verification. Redis rather
	// than process memory so replicas share one count: three replicas each
	// keeping their own would be three times the guesses, which is the whole
	// budget handed back.
	throttle := httpapi.NewFailureThrottle(cfg.RedisAddr, log)

	frontendBase := os.Getenv("FRONTEND_BASE_URL")
	if frontendBase == "" {
		frontendBase = "http://localhost:5173"
	}
	oauthRedirect := os.Getenv("MAIL_OAUTH_REDIRECT_URL")
	if oauthRedirect == "" {
		oauthRedirect = "http://localhost:8080/api/oauth/google/callback"
	}

	srv := &httpapi.Server{
		IAM:                     iamv1.NewAuthServiceClient(iamConn),
		Directory:               iamv1.NewDirectoryServiceClient(iamConn),
		Platform:                iamv1.NewPlatformServiceClient(iamConn),
		Access:                  iamv1.NewAccessServiceClient(iamConn),
		Fx:                      fxv1.NewFxServiceClient(fxConn),
		Customers:               mdv1.NewCustomerServiceClient(mdConn),
		Suppliers:               mdv1.NewSupplierServiceClient(mdConn),
		CreditRatings:           mdv1.NewCreditRatingServiceClient(mdConn),
		Ports:                   mdv1.NewPortServiceClient(mdConn),
		Options:                 mdv1.NewOptionServiceClient(mdConn),
		Numbering:               mdv1.NewNumberingServiceClient(mdConn),
		Approval:                apv1.NewApprovalServiceClient(apConn),
		Catalog:                 pdv1.NewCatalogServiceClient(pdConn),
		Attachments:             pdv1.NewAttachmentServiceClient(pdConn),
		Attributes:              pdv1.NewAttributeServiceClient(pdConn),
		Quotations:              exv1.NewQuotationServiceClient(exConn),
		Contracts:               exv1.NewContractServiceClient(exConn),
		Shipments:               exv1.NewShipmentServiceClient(exConn),
		Receipts:                exv1.NewReceiptServiceClient(exConn),
		Requirements:            prv1.NewRequirementServiceClient(prConn),
		Orders:                  prv1.NewPurchaseOrderServiceClient(prConn),
		Sourcing:                prv1.NewSourcingServiceClient(prConn),
		InquiryTemplates:        prv1.NewInquiryTemplateServiceClient(prConn),
		Stocks:                  ivv1.NewStockServiceClient(ivConn),
		Shipping:                shippingv1.NewShippingServiceClient(shippingConn),
		Emails:                  mailv1.NewEmailServiceClient(ntConn),
		Unlock:                  unlock,
		Idem:                    httpapi.NewIdemStore(cfg.RedisAddr, log),
		Revocations:             revocations,
		Limits:                  httpapi.NewRateLimiter(cfg.RedisAddr, log),
		Throttle:                throttle,
		GoogleClientID:          os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
		OAuthRedirectURL:        oauthRedirect,
		FrontendBaseURL:         frontendBase,
		ProcurementMailSenderID: procurementSenderID,
		// Only when a proxy in front actually overwrites X-Forwarded-For.
		// Off unless said so, because the header is caller-supplied and
		// trusting it without such a proxy makes the per-source login limit
		// spoofable — see clientAddr.
		TrustProxyHeaders: os.Getenv("TRUST_PROXY_HEADERS") == "1",
		CookieSecure:      os.Getenv("COOKIE_SECURE") == "1",
		Live:              live,
		JWTSecret:         cfg.JWTSecret,
		TokenTTL:          cfg.JWTTTL,
		Log:               log,
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

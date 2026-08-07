package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	"github.com/sgao19/erp-go/pkg/blobstore"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/livefeed"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/mail/internal/adapter/grpcin"
	"github.com/sgao19/erp-go/services/mail/internal/adapter/grpcout"
	"github.com/sgao19/erp-go/services/mail/internal/adapter/mailfetch"
	openaiadapter "github.com/sgao19/erp-go/services/mail/internal/adapter/openai"
	"github.com/sgao19/erp-go/services/mail/internal/adapter/provider"
	"github.com/sgao19/erp-go/services/mail/internal/app"
	"github.com/sgao19/erp-go/services/mail/internal/config"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "mail")
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

	// Mailbox credentials are encrypted at rest. Refusing to start without a
	// key is deliberate: the alternative is a service that stores
	// authorisation codes in a way that only looks protected.
	var secrets *app.SecretBox
	if cfg.Provider == "smtp" || cfg.CredKey != "" {
		secrets, err = app.NewSecretBox(cfg.CredKey, cfg.CredKeyVersion)
		if err != nil {
			return fmt.Errorf("mail credential encryption: %w (set MAIL_CRED_KEY to a base64 32-byte key)", err)
		}
	}

	// New-mail hints ride the same live channel every other service uses.
	// Without Redis the hints are simply not sent; mail still syncs.
	var live *livefeed.Publisher
	if cfg.RedisAddr != "" {
		live = livefeed.NewPublisher(cfg.RedisAddr, log)
		defer live.Close()
	}

	blobs := grpcout.NewFiles(files)
	var tables app.TableExtractor
	if cfg.OpenAIAPIKey != "" {
		tables = openaiadapter.NewTableExtractor(
			cfg.OpenAIAPIKey, cfg.OpenAIBaseURL, cfg.OpenAIModel, cfg.OpenAITimeout,
		)
	}
	svc := app.New(pool, app.Deps{
		Numbering: grpcout.NewNumbering(mdConn),
		Directory: grpcout.NewDirectory(iamConn),
		Scopes:    grpcout.NewScopes(iamConn),
		Files:     blobs,
		Tables:    tables,
		Secrets:   secrets,
		Live:      live,
	}, log)
	if cfg.OpenAIAPIKey == "" {
		log.Warn("OPENAI_API_KEY is not set — mail-to-Excel conversion is unavailable")
	} else {
		log.Info("mail-to-Excel conversion is available", "model", cfg.OpenAIModel)
	}

	// Which provider actually puts mail on the wire is configuration, not
	// code. SMTP authenticates as the sending employee, so the adapter needs
	// the service to resolve credentials — hence the two-step wiring.
	sender := provider.Pick(cfg.Provider, svc, blobs, cfg.SendTimeout, log)
	svc.UseProvider(sender)
	if cfg.GoogleClientID != "" {
		svc.UseOAuth(app.OAuthConfig{
			ClientID: cfg.GoogleClientID, ClientSecret: cfg.GoogleClientSecret,
		})
		log.Info("google oauth binding is available")
	}

	// Inbound only exists when there is a real host to poll. A mail host
	// offers no webhook, so this is the only way a customer's reply is ever
	// seen — but polling a dev provider would just be a connection error
	// every two minutes.
	if cfg.Provider == "smtp" {
		imap := mailfetch.NewIMAP(cfg.SyncTimeout, cfg.DialTimeout, log)
		svc.UseMailbox(imap)
		// Connections are kept between commands now, so something has to close
		// the ones nobody is using. Without this the pool only grows.
		go imap.Run(ctx)
		syncCfg := app.SyncConfig{
			Interval:    cfg.SyncInterval,
			BatchSize:   uint32(cfg.SyncBatch),
			HistoryCap:  int64(cfg.SyncHistory),
			Concurrency: cfg.SyncConcurrency,
		}
		// The poller is the historian and the safety net; the idle watchers
		// are what make new mail arrive in seconds instead of minutes.
		go svc.RunInboundSync(ctx, syncCfg)
		go svc.RunIdleWatchers(ctx, syncCfg)
		// The other direction: housekeeping done here reaches the real
		// mailbox. Its own loop rather than a step inside the poller, so a
		// slow fetch never delays publishing a click.
		go svc.RunFlagWriteback(ctx, syncCfg)
		// Trash that clears itself, so "deleted" eventually means deleted
		// without anybody having to remember to empty it.
		go svc.RunTrashSweeper(ctx, syncCfg)
		// Mail stored before search_text existed has none. Derived here with
		// the same function ingest uses rather than by a regexp in the
		// migration, so old and new mail are searched by the same text.
		go svc.BackfillSearchText(ctx, syncCfg)
	}

	// The worker runs in-process. The database is the queue, so a second
	// replica would simply claim different rows - SKIP LOCKED makes that
	// safe without any coordination.
	if cfg.PublicBaseURL == "" && cfg.Provider == "smtp" {
		log.Warn("MAIL_PUBLIC_BASE_URL is not set — outgoing mail carries no open-tracking pixel")
	}
	go svc.RunWorker(ctx, app.WorkerConfig{
		PublicBaseURL:  cfg.PublicBaseURL,
		BatchSize:      cfg.BatchSize,
		SendDelay:      cfg.SendDelay,
		DecisionWindow: cfg.DecisionWindow,
	})

	srv := grpc.NewServer(grpcx.ServerInterceptors(log))
	mailv1.RegisterEmailServiceServer(srv, grpcin.New(svc))
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
	log.Info("mail listening", "port", cfg.GRPCPort, "provider", sender.Name())
	return srv.Serve(lis)
}

func dial(addr string) (*grpc.ClientConn, error) {
	return grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(grpcx.UnaryClientPropagator()))
}

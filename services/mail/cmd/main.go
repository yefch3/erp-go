package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	// The zone database, compiled in.
	//
	// The service image is distroless: no /usr/share/zoneinfo, no package
	// manager to add one. Without this, time.LoadLocation("Asia/Shanghai")
	// fails at runtime and every timestamp in an exported conversation
	// silently falls back to UTC — a transcript saying a customer replied at
	// 03:14 when it was a quarter past eleven in the morning. Roughly 450 kB
	// for a correct clock in a document of record.
	_ "time/tzdata"

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
		// Empty for MinIO (which ignores it); required against real S3 —
		// an unset region signs for us-east-1 and any other region answers
		// 301 to every request, including the startup bucket check.
		Region: os.Getenv("MINIO_REGION"),
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
			Interval:      cfg.SyncInterval,
			BatchSize:     uint32(cfg.SyncBatch),
			HistoryCap:    int64(cfg.SyncHistory),
			Concurrency:   cfg.SyncConcurrency,
			PublicBaseURL: cfg.PublicBaseURL,
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
		// Received mail's pictures, fetched by us at delivery instead of by
		// the reader's browser at reading time — so opening a mail stops
		// being an event the sender observes. Its own loop for the same
		// reason as the write-back: one slow marketing server must not hold
		// up mail arriving. Doubles as the backfill for everything already
		// stored, which has no stamp yet.
		go svc.RunImageCache(ctx, syncCfg)
		// Repairs that re-read the archived MIME, in one goroutine and in this
		// order. The order is the point: each of them fixes a row by parsing
		// the object its raw_key names, so all three are only as trustworthy
		// as the claim that the object is this message. Run concurrently, one
		// of them could re-parse a shared original before the collision repair
		// had disowned it, and write a stranger's mail into the row.
		go func() {
			// Originals that more than one message claims. The key was
			// tenant/account/uid, and a UID is unique within a folder rather
			// than within an account, so INBOX/SENT/JUNK messages sharing a
			// number shared an object and the last one written won.
			svc.RunRawKeyCollisionRepair(ctx, syncCfg)
			// Messages stored before ingest kept Content-ID: their embedded
			// pictures are in storage but nothing joins them to the body that
			// points at them.
			svc.RunContentIDBackfill(ctx, syncCfg)
			// The other half of the same damage: parts ingest never stored at
			// all because they carried a Content-ID but no filename — which is
			// exactly how an image pasted into Gmail's composer arrives. The
			// backfill above cannot help those; it patches rows, and for these
			// there is no row.
			svc.RunEmbeddedRecovery(ctx, syncCfg)
			// And the third variant: not a picture filed wrongly but the body
			// itself. A part with a Content-ID was always taken for an
			// attachment, and LinkedIn puts one on its text/plain and text/html
			// alternatives, so those messages were stored with no text at all.
			// The poller will not revisit them — it advances a UID watermark —
			// so the archived MIME is the only way back.
			svc.RunEmptyBodyRecovery(ctx, syncCfg)
			// Last, and the only one that talks to the host: originals the
			// collision destroyed are re-fetched, found by Message-ID. After
			// the collision repair on purpose — a row must have been disowned
			// before it is worth going back to the host for its original.
			svc.RunRawOriginalRefetch(ctx, syncCfg)
		}()
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
	go svc.RunExcelWorker(ctx)

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

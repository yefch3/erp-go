package main

import (
	"github.com/shopspring/decimal"

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
	"github.com/sgao19/erp-go/services/mail/internal/adapter/gotenberg"
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
	// 单价解析不了就当没配：用量照记，金额留空。绝不因为一个价格填错了
	// 就让邮件服务起不来。
	pricing := app.ModelPricing{Currency: cfg.OpenAIPriceCurrency}
	if v, err := decimal.NewFromString(cfg.OpenAIInputPerMTok); err == nil {
		pricing.InputPerMTok = v
	} else if cfg.OpenAIInputPerMTok != "" {
		log.Warn("OPENAI_INPUT_PER_MTOK is not a number; usage will be reported without a cost")
	}
	if v, err := decimal.NewFromString(cfg.OpenAIOutputPerMTok); err == nil {
		pricing.OutputPerMTok = v
	} else if cfg.OpenAIOutputPerMTok != "" {
		log.Warn("OPENAI_OUTPUT_PER_MTOK is not a number; usage will be reported without a cost")
	}
	// 只配了一半会安静地什么金额都不出（Configured 要求两个价都有）。安静
	// 是对的——半份单价算不出成本——但配的人得知道自己配了一半，否则他会
	// 盯着一个空列去查一个不存在的问题。
	if pricing.InputPerMTok.IsPositive() != pricing.OutputPerMTok.IsPositive() {
		log.Warn("only one of OPENAI_INPUT_PER_MTOK / OPENAI_OUTPUT_PER_MTOK is set; " +
			"cost needs both, so usage will be reported without a cost")
	}

	// 文档转换器。没配地址就没有转换器：Word/Excel 的预览按钮会说没配，别的
	// 一切照常——附件仍然列得出来、下载得下来。
	//
	// **一定要显式判空再赋值。** 直接写 `Converter: gotenberg.New(...)`，
	// 没配地址时塞进去的是一个「装着空指针的非空接口」：app 那边
	// `s.converter == nil` 判假，一路走到空指针上。测试
	// TestANilClientDoesNotPanic 钉的是同一件事的另一半。
	var converter app.Converter
	if c := gotenberg.New(cfg.GotenbergURL, cfg.GotenbergTimeout); c != nil {
		converter = c
	} else {
		log.Warn("GOTENBERG_URL is not set — Word and Excel attachments cannot be previewed")
	}

	svc := app.New(pool, app.Deps{
		Numbering: grpcout.NewNumbering(mdConn),
		Directory: grpcout.NewDirectory(iamConn),
		Scopes:    grpcout.NewScopes(iamConn),
		Files:     blobs,
		Tables:    tables,
		Pricing:   pricing,
		Secrets:   secrets,
		Live:      live,
		Converter: converter,
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
	// 读路径靠它认出自家的追踪像素并拆掉。不给的话，本公司的人打开自己发出
	// 的信就会把对方标成已读。
	svc.UsePublicBaseURL(cfg.PublicBaseURL)
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
			ActiveWindow:  cfg.SyncActiveWindow,
			StatusEvery:   cfg.SyncStatusEvery,
			StatusBudget:  cfg.SyncStatusBudget,
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
		go svc.RunTrashSweeper(ctx)
		// Received mail's pictures, fetched by us at delivery instead of by
		// the reader's browser at reading time — so opening a mail stops
		// being an event the sender observes. Its own loop for the same
		// reason as the write-back: one slow marketing server must not hold
		// up mail arriving.
		//
		// 这一条是**常驻队列**，不是一次性修补：新到的每一封信都会进来一次，
		// 走的是 00029 那条局部索引（images_cached_at IS NULL），补完就不在
		// 索引里了。所以它和下面那些「修历史」的东西不是一回事。
		go svc.RunImageCache(ctx, syncCfg)
		// 入库时原件没存进对象存储的信（写对象失败但行照样入库，见 inbound.go），
		// 回原服务器按 Message-ID 把原件补拉回来。
		//
		// **这里从前还有七条一次性的历史修补**：搜索文本、原件撞键、Content-ID
		// 补全、贴图找回、空正文找回、收件人清单、编码的发件人名。它们各自
		// 修的是某一次解析口径改动之前入库的信，到 2026-09-03 全部跑完，能修的
		// 都修了。留着它们的代价不是「跑一次」，是**每次重启都把整张表扫一遍**：
		// 那几条查询用 LIKE、正则、正文比对，一条索引都用不上；而且每条都拖着
		// 一小撮永远修不好的行（原件没了、本来就没有 To 头），每次重启重试、
		// 每次失败、下次再来。邮件只会越来越多，这个成本只会越来越大。
		//
		// 所以删掉了，不是关掉。新来的信由改好的解析直接写对，不需要事后补。
		// 日后再遇到要修历史的事，就为那件事写一条，修完再删——修补是一次性的，
		// 代码不该越攒越多。
		go svc.RunRawOriginalRefetch(ctx, syncCfg)
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

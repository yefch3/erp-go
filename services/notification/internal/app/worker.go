package app

import (
	"context"
	"time"

	"github.com/sgao19/erp-go/services/notification/internal/store"
)

// Backoff schedule. Greylisting is the reason the first step is not instant:
// a receiving server that deliberately refuses the first attempt is waiting
// to see whether a well-behaved sender comes back.
var backoff = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
	6 * time.Hour,
}

// WorkerConfig tunes the drain loop.
type WorkerConfig struct {
	TenantID int64
	// How often to look for work.
	Interval time.Duration
	// How many to claim per pass. This is the rate limit: providers cap
	// sends per second, and a batch that ignores the cap gets throttled
	// halfway through — which is exactly the "some succeeded, some failed"
	// pattern this whole design exists to avoid.
	BatchSize int32
	// Pause between individual sends inside a batch.
	SendDelay time.Duration
	// How long a message may sit in SENDING before we admit we do not know
	// whether it went out.
	DecisionWindow time.Duration
}

func (c WorkerConfig) withDefaults() WorkerConfig {
	if c.TenantID == 0 {
		c.TenantID = 1
	}
	if c.Interval <= 0 {
		c.Interval = time.Second
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 20
	}
	if c.SendDelay < 0 {
		c.SendDelay = 0
	}
	if c.DecisionWindow <= 0 {
		c.DecisionWindow = 10 * time.Minute
	}
	return c
}

// RunWorker drains the queue until the context is cancelled.
//
// The database is the queue, not Kafka. Delayed retry is one column here and
// a family of retry topics there; "which ones need a person" is a SQL query
// here and impossible there; and the per-recipient status table has to exist
// either way, so putting the work in Kafka too would mean two sources of
// truth. Kafka's job in this system is carrying triggers across services.
func (s *Service) RunWorker(ctx context.Context, cfg WorkerConfig) {
	cfg = cfg.withDefaults()
	s.log.Info("delivery worker started",
		"provider", s.provider.Name(), "batch", cfg.BatchSize,
		"decision_window", cfg.DecisionWindow.String())

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.log.Info("delivery worker stopped")
			return
		case <-ticker.C:
			s.reviveStuck(ctx, cfg)
			if err := s.drainOnce(ctx, cfg); err != nil {
				s.log.Error("delivery pass failed", "err", err)
			}
		}
	}
}

// reviveStuck moves anything abandoned mid-call out of SENDING.
//
// A row left in SENDING means the process died between calling the provider
// and recording the answer — the one case where whether it was sent is
// genuinely unknowable from here. It becomes SEND_UNKNOWN rather than being
// retried, because a silent duplicate is worse than a visible question.
func (s *Service) reviveStuck(ctx context.Context, cfg WorkerConfig) {
	n, err := s.q.ReviveStuckSending(ctx, store.ReviveStuckSendingParams{
		TenantID: cfg.TenantID, WindowSeconds: int32(cfg.DecisionWindow.Seconds()),
	})
	if err != nil {
		s.log.Error("could not sweep stuck sends", "err", err)
		return
	}
	if n > 0 {
		s.log.Warn("messages left SENDING past the decision window", "count", n)
	}
}

func (s *Service) drainOnce(ctx context.Context, cfg WorkerConfig) error {
	// Claiming marks the rows SENDING and commits before any of them is
	// handed to the provider. That ordering is the whole safety property:
	// the row is the only evidence an attempt exists, so it has to exist
	// first.
	batch, err := s.q.ClaimMessages(ctx, store.ClaimMessagesParams{
		TenantID: cfg.TenantID, RowLimit: cfg.BatchSize,
	})
	if err != nil {
		return err
	}
	// Attachments belong to the send, not the recipient, so they are looked
	// up once per campaign in the batch rather than once per message — a
	// 500-recipient send would otherwise repeat the same query 500 times.
	files := map[int64][]Attachment{}
	for _, m := range batch {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		s.deliver(ctx, cfg, m, s.attachmentsOf(ctx, cfg.TenantID, m.CampaignID, files))
		if cfg.SendDelay > 0 {
			time.Sleep(cfg.SendDelay)
		}
	}
	return nil
}

// attachmentsOf memoises within one drain pass.
//
// A failure to read them is logged and treated as "none": a mail that goes
// out without its attachment is recoverable by resending, whereas refusing
// to send at all would strand the whole batch on a storage hiccup.
func (s *Service) attachmentsOf(ctx context.Context, tenantID, campaignID int64, cache map[int64][]Attachment) []Attachment {
	if campaignID == 0 {
		return nil
	}
	if got, ok := cache[campaignID]; ok {
		return got
	}
	rows, err := s.q.ListAttachments(ctx, store.ListAttachmentsParams{
		TenantID: tenantID, CampaignID: campaignID,
	})
	if err != nil {
		s.log.Error("could not read attachments", "campaign_id", campaignID, "err", err)
		cache[campaignID] = nil
		return nil
	}
	out := make([]Attachment, 0, len(rows))
	for _, r := range rows {
		out = append(out, Attachment{
			ID: r.ID, FileName: r.FileName, FileKey: r.FileKey,
			FileSize: r.FileSize, ContentType: r.ContentType,
		})
	}
	cache[campaignID] = out
	return out
}

func (s *Service) deliver(ctx context.Context, cfg WorkerConfig, m store.ClaimMessagesRow, files []Attachment) {
	res := s.provider.Send(ctx, Outbound{
		MessageKey:  m.MessageKey,
		FromName:    m.SenderName,
		ToEmail:     m.ToEmail,
		ToName:      m.ToName,
		Subject:     m.Subject,
		Body:        m.Body,
		BodyText:    m.BodyText,
		Format:      m.BodyFormat,
		Attachments: files,
	})

	switch res.Outcome {
	case Accepted:
		if err := s.q.MarkAccepted(ctx, store.MarkAcceptedParams{
			TenantID: cfg.TenantID, ID: m.ID, ProviderID: res.ProviderID,
		}); err != nil {
			s.log.Error("could not record acceptance", "id", m.ID, "err", err)
			return
		}
		_ = s.q.AppendEvent(ctx, store.AppendEventParams{
			TenantID: cfg.TenantID, MessageID: m.ID, Kind: "SENT",
			Detail: res.ProviderID,
		})

	case Retryable:
		if int(m.AttemptCount) >= len(backoff) {
			// Retries exhausted. This is a queue for a person now, not a
			// failure to file away: an address may be wrong, a mailbox full,
			// a domain misconfigured — all of which somebody can fix.
			s.terminal(ctx, cfg, m.ID, "NEEDS_ATTENTION", res.Err,
				"重试 "+itoa(len(backoff))+" 次仍未成功，请人工处理")
			return
		}
		wait := backoff[m.AttemptCount-1]
		if err := s.q.MarkRetryable(ctx, store.MarkRetryableParams{
			TenantID: cfg.TenantID, ID: m.ID, LastError: res.Err,
			BackoffSeconds: int32(wait.Seconds()),
		}); err != nil {
			s.log.Error("could not schedule retry", "id", m.ID, "err", err)
		}

	case Permanent:
		s.terminal(ctx, cfg, m.ID, "HARD_BOUNCED", res.Err,
			"地址被永久拒收，请核对后更新联系人邮箱")
		_ = s.q.AppendEvent(ctx, store.AppendEventParams{
			TenantID: cfg.TenantID, MessageID: m.ID, Kind: "BOUNCE", Detail: res.Err,
		})
		// A permanently dead address must never be queued again. Continuing
		// to send at one is the fastest way to lose the sending domain's
		// reputation, which costs every other mail too.
		_ = s.q.AddSuppression(ctx, store.AddSuppressionParams{
			TenantID: cfg.TenantID, Email: m.ToEmail,
			Reason: "HARD_BOUNCE", Detail: res.Err,
		})

	case Unknown:
		// The request went out and no answer came back. Which way to fall is
		// a business decision, not a technical one, and it differs by kind:
		// a duplicate marketing mail looks careless and raises complaints,
		// while a quotation that never arrives loses the deal.
		if m.Kind == "TRANSACTIONAL" {
			wait := time.Minute
			if int(m.AttemptCount) < len(backoff) {
				wait = backoff[m.AttemptCount-1]
			}
			s.log.Warn("transactional send unresolved, retrying",
				"id", m.ID, "attempt", m.AttemptCount)
			if err := s.q.MarkRetryable(ctx, store.MarkRetryableParams{
				TenantID: cfg.TenantID, ID: m.ID, LastError: res.Err,
				BackoffSeconds: int32(wait.Seconds()),
			}); err != nil {
				s.log.Error("could not reschedule unresolved send", "id", m.ID, "err", err)
			}
			return
		}
		s.terminal(ctx, cfg, m.ID, "SEND_UNKNOWN", res.Err,
			"发送超时，无法确定是否已发出——请人工确认后决定重发或放弃")
	}
}

func (s *Service) terminal(ctx context.Context, cfg WorkerConfig, id int64, status, lastErr, reason string) {
	if err := s.q.MarkTerminal(ctx, store.MarkTerminalParams{
		TenantID: cfg.TenantID, ID: id, Status: status,
		LastError: lastErr, AttentionReason: reason,
	}); err != nil {
		s.log.Error("could not record terminal state", "id", id, "status", status, "err", err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

package app

import (
	"context"
	"time"

	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// RunEmptyBodyRecovery puts back the text of messages that arrived with none.
//
// Until 2026-08-11 any part carrying a Content-ID was filed as an attachment.
// That rule exists for a picture pasted into a composer - inline, unnamed,
// identified only by its Content-ID - and it is right for one. It is wrong for
// a body: LinkedIn labels its two alternatives Content-ID: text-body and
// html-body, so both were taken away and the message was stored with no text
// at all and two files called attachment.img. Ten messages in one mailbox were
// in that state, every one from LinkedIn.
//
// New mail is parsed correctly now. This pass is for the mail that already
// arrived, which nothing else will revisit: the poller advances a UID
// watermark, so a message it has seen once is never fetched again.
//
// Safe to run repeatedly, and self-clearing: the queue is defined as "no body
// at all", so a message that gets its text back stops matching.
func (s *Service) RunEmptyBodyRecovery(ctx context.Context, cfg SyncConfig) {
	cfg = cfg.withDefaults()
	if s.files == nil {
		return
	}
	repaired, examined := 0, 0
	for {
		rows, err := s.q.ListInboundWithNoBody(ctx, store.ListInboundWithNoBodyParams{
			TenantID: cfg.TenantID, RowLimit: recoveryBatch,
		})
		if err != nil {
			s.log.Warn("body recovery could not read a batch", "err", err)
			return
		}
		if len(rows) == 0 {
			break
		}
		before := repaired
		for _, r := range rows {
			if ctx.Err() != nil {
				return
			}
			examined++
			if s.recoverBody(ctx, cfg.TenantID, r.ID, r.RawKey) {
				repaired++
			}
		}
		if repaired == before {
			// Nothing in this batch could be repaired. The queue is defined by
			// the damage, so a message that cannot be fixed never leaves it -
			// without this the same twenty come back for ever. Genuine cases
			// exist: a message whose original really is empty, or one whose
			// raw copy has been lifecycled out of the bucket.
			s.log.Info("body recovery stopping: the remaining messages cannot be repaired",
				"repaired", repaired, "examined", examined)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(recoveryInterval):
		}
	}
	if repaired > 0 {
		s.log.Info("body recovery finished", "messages", repaired, "examined", examined)
	}
}

// recoverBody re-parses one message and writes back what ingest lost.
func (s *Service) recoverBody(ctx context.Context, tenantID, inboundID int64, rawKey string) bool {
	raw, err := s.readRaw(ctx, rawKey)
	if err != nil {
		s.log.Warn("body recovery could not read the original", "id", inboundID, "err", err)
		return false
	}
	parsed, err := ParseMail(raw)
	if err != nil {
		s.log.Warn("body recovery could not parse the original", "id", inboundID, "err", err)
		return false
	}
	if parsed.BodyHTML == "" && parsed.BodyText == "" {
		// Parsing it again produced the same nothing, so this message is not
		// the bug being repaired - its original is genuinely empty. Left alone
		// rather than rewritten, so it stays visible to a later pass if some
		// other parsing fault turns out to explain it.
		return false
	}

	// The two rows the old rule wrote for the body it mistook for files go
	// first: if the update below fails, a half-repaired message shows its
	// attachments rather than showing neither text nor files.
	if _, err := s.q.DeleteInboundBodyMisfiledAsAttachment(ctx,
		store.DeleteInboundBodyMisfiledAsAttachmentParams{
			TenantID: tenantID, InboundID: inboundID,
		}); err != nil {
		s.log.Warn("body recovery could not clear the misfiled parts", "id", inboundID, "err", err)
		return false
	}

	n, err := s.q.RestoreInboundBody(ctx, store.RestoreInboundBodyParams{
		TenantID: tenantID, ID: inboundID,
		BodyHtml: parsed.BodyHTML, BodyText: parsed.BodyText,
		Snippet: snippetOf(parsed),
		// Recomputed rather than left as it was: the flag was set by the same
		// mistake that stored the body as files, and the re-parse is the only
		// thing that knows whether real attachments remain.
		HasAttachments: len(parsed.Attachments) > 0,
	})
	if err != nil {
		s.log.Warn("body recovery could not write the body back", "id", inboundID, "err", err)
		return false
	}
	return n > 0
}

package app

import (
	"context"
	"fmt"
	"time"

	"github.com/sgao19/erp-go/services/mail/internal/store"
)

const (
	// Small batches: each message means reading its original out of object
	// storage and parsing it again, and this runs beside live mail sync.
	recoveryBatch    = 20
	recoveryInterval = 2 * time.Second
)

// RunEmbeddedRecovery puts back the inline pictures that ingest threw away.
//
// Until 2026-08-10 a part was kept only when it carried a filename. An image
// pasted into Gmail's composer carries none — Content-Disposition: inline with
// no filename= and no name= parameter, identified purely by Content-ID. Those
// parts were read past and dropped while the body kept pointing at them, so
// the reader drew an empty bordered box where the picture should be. A
// customer's photo of a damaged carton arrived as nothing at all.
//
// This is a separate pass from RunContentIDBackfill and not a widening of it,
// because the two repair different damage. That one patches a missing
// identifier onto a row that exists; this one creates rows that were never
// written. It gave up on exactly these messages — `len(rows) == 0` — and the
// comment there blamed senders who "never labelled" their parts. That reading
// was wrong: the parts were labelled, we discarded them.
//
// Safe to run repeatedly. Recovery is keyed on the body still citing a
// Content-ID that nothing satisfies, so a message fixed once drops out of the
// queue on its own.
//
// One tenant at a time. The SyncConfig main.go passes carries no tenant — the
// service has served every company from one process since #216 — and this
// pass used to query with whatever it held, which was 0: no rows, no log, a
// silent no-op in production. See tenantsToRepair.
func (s *Service) RunEmbeddedRecovery(ctx context.Context, cfg SyncConfig) {
	cfg = cfg.withDefaults()
	if s.files == nil {
		return
	}
	for _, tenantID := range s.tenantsToRepair(ctx, cfg) {
		if ctx.Err() != nil {
			return
		}
		s.recoverTenantEmbedded(ctx, tenantID)
	}
}

// recoverTenantEmbedded works through one company's queue. Every return here
// ends this company only; the next one still gets its turn.
func (s *Service) recoverTenantEmbedded(ctx context.Context, tenantID int64) {
	recovered, examined := 0, 0
	for {
		rows, err := s.q.ListInboundWithUnresolvedCID(ctx,
			store.ListInboundWithUnresolvedCIDParams{
				TenantID: tenantID, RowLimit: recoveryBatch,
			})
		if err != nil {
			s.log.Warn("embedded recovery could not read a batch", "tenant", tenantID, "err", err)
			return
		}
		if len(rows) == 0 {
			break
		}
		before := recovered
		for _, r := range rows {
			if ctx.Err() != nil {
				return
			}
			examined++
			if n := s.recoverEmbedded(ctx, tenantID, r.ID, r.AccountID, r.RawKey); n > 0 {
				recovered += n
			}
		}
		if recovered == before {
			// A batch where nothing could be recovered. The queue is defined
			// by the damage, so a message that cannot be repaired never leaves
			// it — without this the same twenty come back for ever. Genuine
			// cases exist: a body citing a part the sender really did omit.
			s.log.Info("embedded recovery stopping: the remaining messages cannot be repaired",
				"tenant", tenantID, "recovered", recovered, "examined", examined)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(recoveryInterval):
		}
	}
	if recovered > 0 {
		s.log.Info("embedded recovery finished", "tenant", tenantID, "pictures", recovered, "messages", examined)
	}
}

// recoverEmbedded re-reads one message and stores the parts ingest dropped.
// Returns how many rows it wrote.
func (s *Service) recoverEmbedded(ctx context.Context, tenantID, inboundID, accountID int64, rawKey string) int {
	raw, err := s.readRaw(ctx, rawKey)
	if err != nil {
		s.log.Warn("embedded recovery could not read the original", "id", inboundID, "err", err)
		return 0
	}
	parsed, err := ParseMail(raw)
	if err != nil {
		s.log.Warn("embedded recovery could not parse the original", "id", inboundID, "err", err)
		return 0
	}

	wrote := 0
	for _, a := range parsed.Attachments {
		if a.ContentID == "" {
			// Ordinary attachments are not this pass's business. One that is
			// genuinely missing is a different bug and should be found as one,
			// not quietly papered over here.
			continue
		}
		// Already there — either it survived ingest or an earlier run of this
		// pass stored it. Checked per part rather than per message because a
		// message can be half repaired.
		n, err := s.q.CountInboundAttachmentWithCID(ctx,
			store.CountInboundAttachmentWithCIDParams{
				TenantID: tenantID, InboundID: inboundID, ContentID: a.ContentID,
			})
		if err != nil {
			s.log.Warn("embedded recovery could not check for an existing part",
				"id", inboundID, "err", err)
			continue
		}
		if n > 0 {
			continue
		}

		// The same key shape ingest uses, so nothing downstream has to know
		// whether a picture arrived on the first pass or the second.
		key := fmt.Sprintf("mail/inbound/%d/%d/att/%d-%s",
			tenantID, accountID, inboundID, safeName(a.FileName))
		if err := s.putRaw(ctx, key, a.Data); err != nil {
			s.log.Warn("embedded recovery could not store a picture",
				"id", inboundID, "err", err)
			continue
		}
		if err := s.q.InsertInboundAttachment(ctx, store.InsertInboundAttachmentParams{
			TenantID: tenantID, InboundID: inboundID, FileName: a.FileName,
			ContentType: a.ContentType, FileSize: int64(len(a.Data)),
			FileKey: key, ContentID: a.ContentID,
		}); err != nil {
			s.log.Warn("embedded recovery could not record a picture",
				"id", inboundID, "err", err)
			continue
		}
		wrote++
	}
	if wrote > 0 {
		s.log.Info("embedded recovery restored pictures", "id", inboundID, "pictures", wrote)
	}
	return wrote
}

package app

import (
	"context"
	"time"

	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// Two-way read state.
//
// Until now the sync was one-way on purpose: the ERP never wrote to the real
// mailbox, so no bug here could damage it. The cost was that the ERP and
// Gmail disagreed the moment anybody touched either — read here, unread
// there — which is not how a mail client behaves. Every real client treats
// the server as the source of truth: it pushes local changes up and reads
// the result back down.
//
// This file is both halves of that. Local changes queue in mail_flag_ops and
// a worker publishes them; a reconcile pass then reads the host's own view
// back over the newest slice of each folder. The order matters: reconcile
// only runs for an account with nothing queued, or it would overwrite a
// local change still waiting to go up.

const (
	// What is being published, and which way. Kept apart because the two
	// flags are independent: a mail can be read and starred, and a pending
	// change to one must never displace a pending change to the other.
	flagSeen    = "SEEN"
	flagFlagged = "FLAGGED"

	opAdd    = "ADD"
	opRemove = "REMOVE"

	// How many messages the reconcile pass re-reads per folder. Enough to
	// cover a browsing session in another client, small enough that the cost
	// stays flat as the mailbox grows.
	reconcileWindow = 200
)

// queueFlagWrite records the intent to publish one message's read state.
//
// Best effort by design: the local change is already committed and is what
// the person sees. A failure to queue means the mailbox stays as it was on
// the host until something touches that mail again — worth a log line, not
// worth failing the click they just made.
func (s *Service) queueFlagWrite(ctx context.Context, tenantID, accountID, employeeID int64, folder string, uid int64, flag string, on bool) {
	op := opRemove
	if on {
		op = opAdd
	}
	if err := s.q.EnqueueFlagOp(ctx, store.EnqueueFlagOpParams{
		TenantID: tenantID, AccountID: accountID, EmployeeID: employeeID,
		Folder: folder, ImapUid: uid, Flag: flag, Op: op,
	}); err != nil {
		s.log.Warn("could not queue a flag write-back",
			"account", accountID, "uid", uid, "err", err)
	}
}

// RunFlagWriteback publishes queued flag changes and then reconciles.
//
// Runs continuously alongside the inbound poller. A short interval because
// this is what makes the ERP feel like a mail client rather than a copy of
// one: mark something read here and it should be read in Gmail seconds
// later, not minutes.
func (s *Service) RunFlagWriteback(ctx context.Context, cfg SyncConfig) {
	cfg = cfg.withDefaults()
	if s.mailbox == nil {
		s.log.Info("flag write-back not started — no mailbox adapter configured")
		return
	}
	s.log.Info("flag write-back started", "every", flagWritebackInterval)

	t := time.NewTicker(flagWritebackInterval)
	defer t.Stop()
	for {
		s.publishFlagOps(ctx, cfg)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

const flagWritebackInterval = 20 * time.Second

// publishFlagOps drains one batch of queued changes.
//
// Grouped by account, folder and operation so a hundred mails marked read in
// one click become one connection and one STORE, rather than a hundred of
// each.
func (s *Service) publishFlagOps(ctx context.Context, cfg SyncConfig) {
	ops, err := s.q.ClaimFlagOps(ctx, store.ClaimFlagOpsParams{
		TenantID: cfg.TenantID, RowLimit: 500,
	})
	if err != nil {
		s.log.Error("could not claim flag write-backs", "err", err)
		return
	}
	if len(ops) == 0 {
		return
	}

	type batchKey struct {
		accountID  int64
		employeeID int64
		folder     string
		flag       string
		op         string
	}
	batches := map[batchKey][]store.ClaimFlagOpsRow{}
	for _, o := range ops {
		k := batchKey{
			accountID: o.AccountID, employeeID: o.EmployeeID,
			folder: o.Folder, flag: o.Flag, op: o.Op,
		}
		batches[k] = append(batches[k], o)
	}

	for k, rows := range batches {
		acct, err := s.ForSender(ctx, cfg.TenantID, k.employeeID)
		if err != nil {
			s.failOps(ctx, rows, err)
			continue
		}
		// The host's own name for the folder, resolved now rather than when
		// the change was queued: it is provider-specific and can change.
		actual := k.folder
		if k.folder != "INBOX" {
			actual, err = s.specialFolderOf(ctx, acct, "junk")
			if err != nil {
				s.failOps(ctx, rows, err)
				continue
			}
		}
		uids := make([]uint32, 0, len(rows))
		for _, r := range rows {
			uids = append(uids, uint32(r.ImapUid))
		}
		imapFlag := `\Seen`
		if k.flag == flagFlagged {
			imapFlag = `\Flagged`
		}
		if err := s.mailbox.SetFlags(ctx, acct, actual, uids, imapFlag, k.op == opAdd); err != nil {
			s.log.Warn("flag write-back failed",
				"account", k.accountID, "folder", actual, "flag", k.flag,
				"n", len(uids), "err", err)
			s.failOps(ctx, rows, err)
			continue
		}
		for _, r := range rows {
			if err := s.q.DeleteFlagOp(ctx, r.ID); err != nil {
				s.log.Warn("could not clear a published flag write-back", "id", r.ID, "err", err)
			}
		}
		s.log.Info("flags published to the mail host",
			"account", k.accountID, "folder", actual,
			"flag", k.flag, "op", k.op, "n", len(uids))
	}
}

func (s *Service) failOps(ctx context.Context, rows []store.ClaimFlagOpsRow, cause error) {
	for _, r := range rows {
		if err := s.q.FailFlagOp(ctx, store.FailFlagOpParams{
			ID: r.ID, LastError: truncate(cause.Error(), 500),
		}); err != nil {
			s.log.Warn("could not record a failed flag write-back", "id", r.ID, "err", err)
		}
	}
}

// ReconcileFlags pulls the host's read state back over the newest messages of
// one folder, and is where somebody else's Gmail session reaches the ERP.
//
// Refuses to run while anything is queued for that account: the queue holds
// changes the host has not seen yet, so the host's answer is stale by
// definition until it drains.
func (s *Service) ReconcileFlags(ctx context.Context, tenantID int64, acct MailAccount, folder, actual string) error {
	pending, err := s.q.CountPendingFlagOps(ctx, store.CountPendingFlagOpsParams{
		TenantID: tenantID, AccountID: acct.AccountID,
	})
	if err != nil {
		return err
	}
	if pending > 0 {
		return nil
	}

	rows, err := s.q.ListRecentUIDs(ctx, store.ListRecentUIDsParams{
		TenantID: tenantID, AccountID: acct.AccountID, Folder: folder,
		RowLimit: reconcileWindow,
	})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	uids := make([]uint32, 0, len(rows))
	for _, r := range rows {
		uids = append(uids, uint32(r.ImapUid))
	}

	live, err := s.mailbox.FetchFlags(ctx, acct, actual, uids)
	if err != nil {
		return err
	}
	changed := 0
	for _, r := range rows {
		fl, ok := live[uint32(r.ImapUid)]
		// A UID the host no longer returns was deleted elsewhere. Left alone
		// here: removing rows is deletion, and deletion is its own step with
		// its own rules, not a side effect of reading flags.
		if !ok {
			continue
		}
		if fl.Seen != r.IsRead {
			if err := s.q.SetInboundReadByUID(ctx, store.SetInboundReadByUIDParams{
				TenantID: tenantID, AccountID: acct.AccountID, Folder: folder,
				ImapUid: r.ImapUid, IsRead: fl.Seen,
			}); err != nil {
				s.log.Warn("could not apply the host's read state", "uid", r.ImapUid, "err", err)
				continue
			}
			changed++
		}
		if fl.Flagged != r.IsStarred {
			if err := s.q.SetInboundStarredByUID(ctx, store.SetInboundStarredByUIDParams{
				TenantID: tenantID, AccountID: acct.AccountID, Folder: folder,
				ImapUid: r.ImapUid, IsStarred: fl.Flagged,
			}); err != nil {
				s.log.Warn("could not apply the host's star", "uid", r.ImapUid, "err", err)
				continue
			}
			changed++
		}
	}
	if changed > 0 {
		s.log.Info("flags taken from the mail host",
			"account", acct.AccountID, "folder", folder, "changed", changed)
	}
	return nil
}

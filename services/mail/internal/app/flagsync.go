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

	// How deep to look in the trash and the archive when working out where a
	// message went. Deeper than the reconcile window on purpose: several
	// messages can leave the inbox for the same folder at once.
	departureScan = 400
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
	var moves []store.ClaimFlagOpsRow
	for _, o := range ops {
		// A flag change is the same command whoever it is for, so those batch.
		// A move has to locate its message first, so each stands alone.
		if o.Flag != flagSeen && o.Flag != flagFlagged {
			moves = append(moves, o)
			continue
		}
		k := batchKey{
			accountID: o.AccountID, employeeID: o.EmployeeID,
			folder: o.Folder, flag: o.Flag, op: o.Op,
		}
		batches[k] = append(batches[k], o)
	}

	for _, row := range moves {
		acct, err := s.ForSender(ctx, cfg.TenantID, row.EmployeeID)
		if err != nil {
			s.failOps(ctx, []store.ClaimFlagOpsRow{row}, err)
			continue
		}
		if err := s.publishMove(ctx, acct, row); err != nil {
			s.log.Warn("folder move failed", "account", row.AccountID,
				"flag", row.Flag, "op", row.Op, "uid", row.ImapUid, "err", err)
			s.failOps(ctx, []store.ClaimFlagOpsRow{row}, err)
			continue
		}
		if err := s.q.DeleteFlagOp(ctx, row.ID); err != nil {
			s.log.Warn("could not clear a published move", "id", row.ID, "err", err)
		}
		s.log.Info("mail moved on the host", "account", row.AccountID,
			"flag", row.Flag, "op", row.Op, "uid", row.ImapUid)
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
// followDepartures says whether a message that left this folder should be
// chased down. True for the inbox, where leaving means deleted or archived.
// False for the junk folder, where the usual reason a message leaves is that
// somebody called it "not spam" in Gmail and it went to the inbox — reading
// that as a deletion would throw away exactly the mail the person just
// rescued.
func (s *Service) ReconcileFlags(ctx context.Context, tenantID int64, acct MailAccount, folder, actual string, followDepartures bool) error {
	pending, err := s.q.CountPendingFlagOps(ctx, store.CountPendingFlagOpsParams{
		TenantID: tenantID, AccountID: acct.AccountID,
	})
	if err != nil {
		return err
	}
	if pending > 0 {
		return nil
	}

	rows, err := s.q.ListRecentForReconcile(ctx, store.ListRecentForReconcileParams{
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
	var missing []store.ListRecentForReconcileRow
	for _, r := range rows {
		fl, ok := live[uint32(r.ImapUid)]
		// A UID the host stopped returning means the message left this folder:
		// deleted, archived, or filed somewhere by a rule. Which one it was
		// takes looking, so they are collected and answered together below.
		if !ok {
			missing = append(missing, r)
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
	if followDepartures && len(missing) > 0 {
		s.mirrorDepartures(ctx, tenantID, acct, folder, missing)
	}
	return nil
}

// mirrorDepartures works out what happened to mail that is no longer in the
// folder we last saw it in, and makes the ERP agree.
//
// Two folder scans answer it for the whole batch: a message now in the host's
// trash was deleted, one in the archive was archived, and one in neither is
// gone for good — deleted somewhere and already expunged, or filed into a
// folder the ERP does not track.
//
// All three land as ERP-side state written directly, never through the
// marking path: that would queue a write-back and ask the host to redo what
// the host just did. And a departure is mirrored as a *soft* delete even when
// the message is gone from the host entirely — our copy may be the only one
// left, the trash gives thirty days to notice a mistake, and the sweeper
// finishes the job afterwards.
func (s *Service) mirrorDepartures(ctx context.Context, tenantID int64, acct MailAccount, folder string, missing []store.ListRecentForReconcileRow) {
	trash, err := s.specialFolderOf(ctx, acct, "trash")
	if err != nil {
		s.log.Warn("could not locate the trash while reconciling", "err", err)
		return
	}
	inTrash, err := s.mailbox.RecentMessageIDs(ctx, acct, trash, departureScan)
	if err != nil {
		s.log.Warn("could not read the host's trash while reconciling", "err", err)
		return
	}
	// The archive is optional: a host without one simply never archives.
	inArchive := map[string]bool{}
	if archive, err := s.specialFolderOf(ctx, acct, "archive"); err == nil && archive != "" {
		if ids, err := s.mailbox.RecentMessageIDs(ctx, acct, archive, departureScan); err == nil {
			inArchive = ids
		} else {
			s.log.Warn("could not read the host's archive while reconciling", "err", err)
		}
	}

	deleted, archived, vanished := 0, 0, 0
	for _, r := range missing {
		switch {
		case r.MessageID != "" && inTrash[r.MessageID]:
			if r.DeletedAt.Valid {
				continue
			}
			if err := s.q.MirrorHostDelete(ctx, store.MirrorHostDeleteParams{
				TenantID: tenantID, AccountID: acct.AccountID,
				Folder: folder, ImapUid: r.ImapUid,
			}); err != nil {
				s.log.Warn("could not mirror a deletion", "uid", r.ImapUid, "err", err)
				continue
			}
			deleted++

		case r.MessageID != "" && inArchive[r.MessageID]:
			if r.ArchivedAt.Valid {
				continue
			}
			if err := s.q.MirrorHostArchive(ctx, store.MirrorHostArchiveParams{
				TenantID: tenantID, AccountID: acct.AccountID,
				Folder: folder, ImapUid: r.ImapUid,
			}); err != nil {
				s.log.Warn("could not mirror an archive", "uid", r.ImapUid, "err", err)
				continue
			}
			archived++

		default:
			// Nowhere we can see: expunged elsewhere, or filed into a folder
			// this ERP does not track. Treated as a deletion, softly, and
			// counted apart from the ones actually found in the trash —
			// somebody asking "why is my mail in the bin?" deserves to be
			// able to tell a confirmed deletion from an inference.
			if r.DeletedAt.Valid {
				continue
			}
			if err := s.q.MirrorHostDelete(ctx, store.MirrorHostDeleteParams{
				TenantID: tenantID, AccountID: acct.AccountID,
				Folder: folder, ImapUid: r.ImapUid,
			}); err != nil {
				s.log.Warn("could not mirror a disappearance", "uid", r.ImapUid, "err", err)
				continue
			}
			s.log.Info("mail is no longer anywhere the host will show us",
				"account", acct.AccountID, "uid", r.ImapUid, "message_id", r.MessageID)
			vanished++
		}
	}
	if deleted > 0 || archived > 0 || vanished > 0 {
		s.log.Info("mail followed from the host",
			"account", acct.AccountID, "folder", folder,
			"in_trash", deleted, "archived", archived, "vanished", vanished)
	}
}

// Folder-level publishing: deletion, restore, archiving and permanent
// deletion. Unlike a flag, these move a message — and a move changes its UID,
// so anything that has to find the message afterwards searches by Message-ID.
const (
	flagTrash   = "TRASH"
	flagArchive = "ARCHIVE"
	flagPurge   = "PURGE"
	// Rescuing a mail the host called spam. A move like any other, but worth
	// its own name: it is also the one action that teaches the provider's
	// filter it got this sender wrong, which is half the point of doing it.
	flagNotJunk = "NOTJUNK"
)

// queueFolderMove records the intent to move one message on the host.
func (s *Service) queueFolderMove(ctx context.Context, tenantID, accountID, employeeID int64, folder string, uid int64, messageID, flag string, on bool) {
	op := opRemove
	if on {
		op = opAdd
	}
	if err := s.q.EnqueueFlagOp(ctx, store.EnqueueFlagOpParams{
		TenantID: tenantID, AccountID: accountID, EmployeeID: employeeID,
		Folder: folder, ImapUid: uid, Flag: flag, Op: op, MessageID: messageID,
	}); err != nil {
		s.log.Warn("could not queue a folder move",
			"account", accountID, "uid", uid, "flag", flag, "err", err)
	}
}

// publishMove carries out one folder-level change against the host.
//
// Each op is handled on its own rather than batched: a move needs the
// message located first, and the searches differ per message.
func (s *Service) publishMove(ctx context.Context, acct MailAccount, row store.ClaimFlagOpsRow) error {
	switch row.Flag {
	case flagTrash:
		trash, err := s.specialFolderOf(ctx, acct, "trash")
		if err != nil {
			return err
		}
		if row.Op == opAdd {
			// Straight out of the folder it is still sitting in.
			return s.mailbox.MoveMessages(ctx, acct, row.Folder, []uint32{uint32(row.ImapUid)}, trash)
		}
		// Restoring: it left the source folder when it was deleted, so it has
		// to be found in the trash by Message-ID before it can come back.
		return s.moveBack(ctx, acct, trash, row.Folder, row.MessageID)

	case flagArchive:
		archive, err := s.specialFolderOf(ctx, acct, "archive")
		if err != nil {
			return err
		}
		if archive == "" {
			// This host has no archive — 263 and most plain IMAP servers.
			// The ERP keeps its own archive view and the mailbox is left
			// exactly as it was, which is better than inventing a folder in
			// somebody's mailbox to satisfy our own vocabulary.
			s.log.Info("host has no archive folder; archiving stays ERP-side",
				"account", acct.AccountID)
			return nil
		}
		if row.Op == opAdd {
			return s.mailbox.MoveMessages(ctx, acct, row.Folder, []uint32{uint32(row.ImapUid)}, archive)
		}
		return s.moveBack(ctx, acct, archive, row.Folder, row.MessageID)

	case flagNotJunk:
		junk, err := s.specialFolderOf(ctx, acct, "junk")
		if err != nil {
			return err
		}
		if err := s.mailbox.MoveMessages(ctx, acct, junk, []uint32{uint32(row.ImapUid)}, "INBOX"); err != nil {
			return err
		}
		// The message now lives in the inbox under a new UID. Following it is
		// not optional here: leave the row pointing at the old spam UID and
		// the next sync sees an unknown message in the inbox and files it a
		// second time.
		return s.repoint(ctx, acct, row, "INBOX")

	case flagPurge:
		// Deleted mail is in the trash by now, and that is where it has to be
		// destroyed. If it is not there — already purged, or emptied by hand
		// in Gmail — there is nothing left to do and nothing to report.
		trash, err := s.specialFolderOf(ctx, acct, "trash")
		if err != nil {
			return err
		}
		uid, ok, err := s.mailbox.FindUIDByMessageID(ctx, acct, trash, row.MessageID)
		if err != nil {
			return err
		}
		if !ok {
			// Worth saying out loud rather than passing silently: permanent
			// deletion is the one operation nobody can check afterwards, so
			// "there was nothing there" and "it is gone now" should not look
			// the same in the log.
			s.log.Info("nothing left to purge on the host",
				"account", acct.AccountID, "message_id", row.MessageID)
			return nil
		}
		if err := s.mailbox.PurgeMessages(ctx, acct, trash, []uint32{uid}); err != nil {
			return err
		}
		s.log.Info("purged from the host for good",
			"account", acct.AccountID, "folder", trash, "uid", uid)
		return nil
	}
	return nil
}

// repoint updates our record of where a message lives after we moved it.
//
// Best effort: a failure here costs a duplicate row on the next sync, not the
// move itself, and the move has already happened.
func (s *Service) repoint(ctx context.Context, acct MailAccount, row store.ClaimFlagOpsRow, newFolder string) error {
	uid, ok, err := s.mailbox.FindUIDByMessageID(ctx, acct, newFolder, row.MessageID)
	if err != nil || !ok {
		s.log.Warn("moved a mail but could not find its new UID",
			"account", acct.AccountID, "to", newFolder, "err", err)
		return nil
	}
	if err := s.q.RepointInbound(ctx, store.RepointInboundParams{
		TenantID: row.TenantID, AccountID: row.AccountID,
		OldFolder: row.Folder, OldUid: row.ImapUid,
		NewFolder: newFolder, NewUid: int64(uid),
	}); err != nil {
		s.log.Warn("could not repoint a moved mail", "id", row.ID, "err", err)
	}
	return nil
}

// moveBack returns a message from where it was filed to where it came from.
func (s *Service) moveBack(ctx context.Context, acct MailAccount, from, to, messageID string) error {
	uid, ok, err := s.mailbox.FindUIDByMessageID(ctx, acct, from, messageID)
	if err != nil {
		return err
	}
	if !ok {
		// Not where we filed it: somebody moved or deleted it in another
		// client. Their action is the newer one; ours has nothing left to
		// act on.
		return nil
	}
	return s.mailbox.MoveMessages(ctx, acct, from, []uint32{uid}, to)
}

package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

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
		for _, tenantID := range s.tenantsToServe(ctx) {
			pass := cfg
			pass.TenantID = tenantID
			s.publishFlagOps(ctx, pass)
		}
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
//
// Order matters, and it is flags first. A flag is one STORE on a connection
// that is going to be opened anyway; a move has to find its message before it
// can move it. Running the moves first meant that emptying the trash — which
// queues one op per mail — put every subsequent star and read-mark behind
// several minutes of folder work. The person who starred a mail and then
// looked at Gmail saw nothing there, and concluded, reasonably, that the
// write-back was broken.
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

	// 键里不再有 employeeID：取凭据认的是账号。一个人绑两个箱时，拿 A 箱的
	// UID 去 B 箱上执行——而下面的 publishMove 是**按 UID 移动**，UID 是每个
	// 信箱各自独立的小整数——会把 B 箱里一封无关的邮件扔进垃圾箱。
	type batchKey struct {
		accountID int64
		folder    string
		flag      string
		op        string
	}
	batches := map[batchKey][]store.ClaimFlagOpsRow{}
	// Purges are separated from the other moves because they are the only
	// kind that arrives in bulk — 清空回收站 queues one per mail — and the only
	// kind that can be answered for the whole batch at once.
	purges := map[int64][]store.ClaimFlagOpsRow{}
	var moves []store.ClaimFlagOpsRow
	for _, o := range ops {
		switch {
		// A flag change is the same command whoever it is for, so those batch.
		case o.Flag == flagSeen || o.Flag == flagFlagged:
			k := batchKey{
				accountID: o.AccountID,
				folder:    o.Folder, flag: o.Flag, op: o.Op,
			}
			batches[k] = append(batches[k], o)
		case o.Flag == flagPurge:
			purges[o.AccountID] = append(purges[o.AccountID], o)
		default:
			moves = append(moves, o)
		}
	}

	for k, rows := range batches {
		acct, err := s.ForAccount(ctx, cfg.TenantID, k.accountID)
		if err != nil {
			s.failOps(ctx, rows, err)
			continue
		}
		// The host's own name for the folder, resolved now rather than when
		// the change was queued: it is provider-specific and can change.
		actual, err := s.hostFolder(ctx, acct, k.folder)
		if err != nil {
			s.failOps(ctx, rows, err)
			continue
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

	for accountID, rows := range purges {
		acct, err := s.ForAccount(ctx, cfg.TenantID, accountID)
		if err != nil {
			s.failOps(ctx, rows, err)
			continue
		}
		s.publishPurges(ctx, acct, rows)
	}

	for _, row := range moves {
		acct, err := s.ForAccount(ctx, cfg.TenantID, row.AccountID)
		if err != nil {
			s.failOrRetire(ctx, row, err)
			continue
		}
		if err := s.publishMove(ctx, acct, row); err != nil {
			if s.moveIsMoot(ctx, cfg.TenantID, acct, row) {
				// 信已经不在原文件夹里了——别的客户端先动了手。要的结果（它不
				// 在收件箱里）已经达到，这条操作没有意义了，作废。生产上真发生
				// 过：员工在 Foxmail 里删了，我们拿着一个不存在的 UID 重试到
				// 1265 次，期间这个账号的读状态对账一直被它挡着。
				s.log.Info("folder move retired: message already gone from the host",
					"account", row.AccountID, "flag", row.Flag, "uid", row.ImapUid)
				if err := s.q.DeleteFlagOp(ctx, row.ID); err != nil {
					s.log.Warn("could not clear a moot move", "id", row.ID, "err", err)
				}
				continue
			}
			s.log.Warn("folder move failed", "account", row.AccountID,
				"flag", row.Flag, "op", row.Op, "uid", row.ImapUid, "err", err)
			s.failOrRetire(ctx, row, err)
			continue
		}
		if err := s.q.DeleteFlagOp(ctx, row.ID); err != nil {
			s.log.Warn("could not clear a published move", "id", row.ID, "err", err)
		}
		s.log.Info("mail moved on the host", "account", row.AccountID,
			"flag", row.Flag, "op", row.Op, "uid", row.ImapUid)
	}
}

// publishPurges destroys a whole batch of mail on the host over two
// connections instead of two per message.
//
// Permanent deletion is the one operation that reliably arrives in bulk, and
// it was also the most expensive: locating a message by Message-ID opened a
// connection, and expunging it opened another. Emptying a trash of forty
// mails therefore meant eighty separate dial-authenticate-logout cycles
// against Gmail, in a burst. Gmail answers a burst like that by refusing —
// "Invalid credentials", or by dropping the connection mid-command — and a
// refused connection is then retried on a widening backoff, which is how a
// single click turned into minutes of nothing appearing to happen.
//
// One search connection for the whole batch, one expunge for the whole batch.
func (s *Service) publishPurges(ctx context.Context, acct MailAccount, rows []store.ClaimFlagOpsRow) {
	trash, err := s.specialFolderOf(ctx, acct, "trash")
	if err != nil {
		for _, r := range rows {
			s.failOrRetire(ctx, r, err)
		}
		return
	}
	// 先用挪进回收站时记下来的 UID；只有没记录的才去按 Message-ID 搜。
	// 263 不认那种搜索，所以对 263 来说这一步就是"能不能彻底删除"的分界。
	uids := make([]uint32, 0, len(rows))
	var inTrash, elsewhere, unknown []store.ClaimFlagOpsRow
	for _, r := range rows {
		if uid, ok := s.knownHostUID(ctx, r, trash); ok {
			uids = append(uids, uid)
			inTrash = append(inTrash, r)
			continue
		}
		unknown = append(unknown, r)
	}
	if len(unknown) > 0 {
		ids := make([]string, 0, len(unknown))
		for _, r := range unknown {
			if r.MessageID != "" {
				ids = append(ids, r.MessageID)
			}
		}
		found, err := s.mailbox.FindUIDsByMessageIDs(ctx, acct, trash, ids)
		if err != nil {
			// 这一支也要走带上限的版本。263 不认 HEADER Message-Id 的 SEARCH
			// （"can't search that criteria"）：从前走到这里的操作永远到不了
			// MOVE、也就永远碰不到别处的上限。有记录的那些不受搜索失败连累。
			for _, r := range unknown {
				s.failOrRetire(ctx, r, err)
			}
			unknown = nil
		}
		for _, r := range unknown {
			uid, ok := found[r.MessageID]
			if !ok {
				// Not in the trash. Usually somebody got there first, but it can
				// also mean the move that should have put it there never ran —
				// publishMove's PURGE branch handles both, one message at a time.
				elsewhere = append(elsewhere, r)
				continue
			}
			uids = append(uids, uid)
			inTrash = append(inTrash, r)
		}
	}

	if len(uids) > 0 {
		if err := s.mailbox.PurgeMessages(ctx, acct, trash, uids); err != nil {
			s.log.Warn("batch purge failed", "account", acct.AccountID,
				"n", len(uids), "err", err)
			// 逐条走带上限的版本：一次总是失败的批量清理，不能像从前那样
			// 把整批操作永远留在队列里，顺带封死这个账号的读状态对账。
			for _, r := range inTrash {
				s.failOrRetire(ctx, r, err)
			}
		} else {
			for _, r := range inTrash {
				if err := s.q.DeleteFlagOp(ctx, r.ID); err != nil {
					s.log.Warn("could not clear a published purge", "id", r.ID, "err", err)
				}
			}
			s.log.Info("purged from the host for good",
				"account", acct.AccountID, "folder", trash, "n", len(uids))
		}
	}

	for _, r := range elsewhere {
		if err := s.publishMove(ctx, acct, r); err != nil {
			s.log.Warn("folder move failed", "account", r.AccountID,
				"flag", r.Flag, "op", r.Op, "uid", r.ImapUid, "err", err)
			s.failOrRetire(ctx, r, err)
			continue
		}
		if err := s.q.DeleteFlagOp(ctx, r.ID); err != nil {
			s.log.Warn("could not clear a published move", "id", r.ID, "err", err)
		}
	}
}

// maxFlagOpAttempts 是一条写回操作最多试几次。
//
// 退避只是让失败变慢，不让它停：一条永远失败的操作按最长 12 分钟一次，一天
// 仍然是 120 次连接，而且只要它还在队列里，CountPendingFlagOps 就大于零，这个
// 账号的读状态对账（ReconcileFlags）就一直不跑。生产上有三条操作分别重试了
// 992、993、1265 次，对应的账号从 8 月 25 日起就没再和 Foxmail 对过已读。
//
// 20 次按现在的退避约合三四个小时：一次真正的临时故障（服务器抖一下、网络
// 断一会儿）早就过去了；还在失败的，就不是临时的。
const maxFlagOpAttempts = 20

// failOrRetire 记一次失败；到了上限就放弃，而不是永远重试。
func (s *Service) failOrRetire(ctx context.Context, row store.ClaimFlagOpsRow, cause error) {
	if row.Attempts+1 >= maxFlagOpAttempts {
		s.log.Warn("folder move given up after repeated failures",
			"account", row.AccountID, "flag", row.Flag, "op", row.Op,
			"uid", row.ImapUid, "attempts", row.Attempts+1, "err", cause)
		if err := s.q.DeleteFlagOp(ctx, row.ID); err != nil {
			s.log.Warn("could not retire a failed move", "id", row.ID, "err", err)
		}
		return
	}
	s.failOps(ctx, []store.ClaimFlagOpsRow{row}, cause)
}

// moveIsMoot 问服务器：这封信还在原文件夹里吗。
//
// 只对「挪出去」的操作有意义（删除、归档）。挪回来（恢复）走 Message-ID
// 查找，本来就不依赖旧 UID。问不到时按「还在」处理——宁可多重试，也不
// 因为一次网络抖动把一条正当的删除作废掉。
//
// **先看 UIDVALIDITY。** 文件夹换代之后我们手里的 UID 全部作废，按 UID 去查
// 每一封都"不在"——不是信没了，是编号没意义了。这时不能作废操作；让它按
// 退避走到上限、留一条告警，人还能看见。静默作废是所有结果里最坏的一种。
func (s *Service) moveIsMoot(ctx context.Context, tenantID int64, acct MailAccount, row store.ClaimFlagOpsRow) bool {
	if row.Op != opAdd || (row.Flag != flagTrash && row.Flag != flagArchive) {
		return false
	}
	home, err := s.hostFolder(ctx, acct, row.Folder)
	if err != nil {
		return false
	}
	state, err := s.q.GetSyncState(ctx, store.GetSyncStateParams{
		TenantID: tenantID, AccountID: acct.AccountID, Folder: row.Folder,
	})
	if err != nil {
		return false
	}
	st, err := s.mailbox.FolderStatus(ctx, acct, home)
	if err != nil || st.UIDValidity == 0 || int64(st.UIDValidity) != state.UidValidity {
		return false
	}
	live, err := s.mailbox.FetchFlags(ctx, acct, home, []uint32{uint32(row.ImapUid)})
	if err != nil {
		return false
	}
	_, stillThere := live[uint32(row.ImapUid)]
	return !stillThere
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
//
// rescue names a folder where finding a departed message means nothing
// happened to it. Only the junk folder needs one, and it needs one badly: mail
// usually leaves spam because somebody called it "not spam" in Gmail, and it
// lands in the inbox. Empty for folders with no such destination.
func (s *Service) ReconcileFlags(ctx context.Context, tenantID int64, acct MailAccount, folder, actual, rescue string) error {
	pending, err := s.q.CountPendingFlagOps(ctx, store.CountPendingFlagOpsParams{
		TenantID: tenantID, AccountID: acct.AccountID,
	})
	if err != nil {
		return err
	}
	if pending > 0 {
		return nil
	}

	// Stars first, and over the whole folder rather than the window below.
	// A failure here must not cost the read-state pass, which is the more
	// consequential of the two.
	if err := s.reconcileStars(ctx, tenantID, acct, folder, actual); err != nil {
		s.log.Warn("could not take the host's stars", "folder", folder, "err", err)
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
		// Stars are not compared here. reconcileStars has already settled them
		// for the whole folder; repeating the check against the values read
		// before that ran would only rewrite rows it just fixed.
	}
	if changed > 0 {
		s.log.Info("flags taken from the mail host",
			"account", acct.AccountID, "folder", folder, "changed", changed)
	}
	if len(missing) > 0 {
		s.mirrorDepartures(ctx, tenantID, acct, folder, rescue, missing)
	}
	return nil
}

// reconcileStars makes the ERP's stars match the host's across a whole folder.
//
// Separate from the read-state pass because the two have different natural
// shapes. Read state has to be asked message by message — it changes on almost
// everything and there is no cheap way to ask "which of these are unread"
// except to fetch their flags. Stars are rare and the server can name them
// all: one SEARCH, one UPDATE, no window.
//
// That window was the bug. Riding along with the 200-UID flag fetch meant the
// ERP only ever learned about stars on the last few days of mail, so a mailbox
// with a dozen starred threads showed one.
func (s *Service) reconcileStars(ctx context.Context, tenantID int64, acct MailAccount, folder, actual string) error {
	uids, err := s.mailbox.SearchFlagged(ctx, acct, actual)
	if err != nil {
		return err
	}
	// An empty result is a real answer — "nothing is starred any more" — and
	// has to be applied, or unstarring the last one in Gmail would never
	// reach here. ANY('{}') is false for every row, which is exactly right.
	starred := make([]int64, 0, len(uids))
	for _, u := range uids {
		starred = append(starred, int64(u))
	}
	n, err := s.q.SyncStarredFromHost(ctx, store.SyncStarredFromHostParams{
		TenantID: tenantID, AccountID: acct.AccountID, Folder: folder,
		StarredUids: starred,
	})
	if err != nil {
		return err
	}
	if n > 0 {
		s.log.Info("stars taken from the mail host",
			"account", acct.AccountID, "folder", folder,
			"starred_on_host", len(starred), "changed", n)
	}
	return nil
}

// mirrorDepartures works out what happened to mail that is no longer in the
// folder we last saw it in, and makes the ERP agree.
//
// Folder scans answer it for the whole batch: a message now in the host's
// trash was deleted, one in the archive was archived, one in the caller's
// rescue folder was never deleted at all, and one in none of them is gone for
// good — deleted somewhere and already expunged, or filed into a folder the
// ERP does not track.
//
// The rescue folder is what lets the junk folder take part at all. Departures
// there used to be ignored outright, on the grounds that mail leaves spam
// mostly because somebody rescued it and calling that a deletion would bin the
// message they just saved. True, but too blunt: it also meant that deleting a
// spam in Gmail reached nothing, so the ERP went on listing junk the mailbox
// no longer had — and Gmail purges spam by itself after thirty days, so the
// ERP's junk folder filled up with mail that had not existed for a month.
// Looking in the inbox separates the two cases instead of giving up on both.
//
// All of it lands as ERP-side state written directly, never through the
// marking path: that would queue a write-back and ask the host to redo what
// the host just did.
//
// A first departure is always mirrored as a *soft* delete, so a wrong guess
// costs a trip to the recycle bin rather than the mail. The second one is not:
// a message already in our bin that the host cannot produce any more has been
// destroyed there, and the mirror is strict, so it is destroyed here — objects
// and row. That step alone refuses to act on the folder scans and demands a
// targeted search first; see the branch for why.
func (s *Service) mirrorDepartures(ctx context.Context, tenantID int64, acct MailAccount, folder, rescue string, missing []store.ListRecentForReconcileRow) {
	// Asked for first, and fatal when it fails. Without this answer a rescue
	// is indistinguishable from a deletion, and of the two mistakes available
	// the one to avoid is binning mail somebody just saved.
	rescued := map[string]bool{}
	if rescue != "" {
		ids, err := s.mailbox.RecentMessageIDs(ctx, acct, rescue, departureScan)
		if err != nil {
			s.log.Warn("could not read the rescue folder while reconciling",
				"folder", folder, "rescue", rescue, "err", err)
			return
		}
		rescued = ids
	}

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

	// 在网页端把信标为垃圾，是收件箱信件消失的另一个常见去向。原来这里不查
	// 垃圾箱，这种离开被当成「vanished → 软删」——收件箱行进了回收站，垃圾箱
	// 同步又把同一封信另立一行，会话里两条。只在收件箱这一趟查：别的文件夹
	// 的信不会被「标为垃圾」挪走，白扫一遍是纯开销。
	inJunk := map[string]bool{}
	junkName := ""
	if folder == "INBOX" {
		if j, err := s.specialFolderOf(ctx, acct, "junk"); err == nil && j != "" {
			if ids, err := s.mailbox.RecentMessageIDs(ctx, acct, j, departureScan); err == nil {
				junkName, inJunk = j, ids
			} else {
				s.log.Warn("could not read the host's junk while reconciling", "err", err)
			}
		}
	}

	deleted, archived, vanished, saved, junked, purged := 0, 0, 0, 0, 0, 0
	for _, r := range missing {
		switch {
		// Checked before the trash, because it is the case that must never be
		// misread: somebody clicked "not spam" in their webmail and the mail
		// is in the inbox now.
		//
		// 原来这里只数一下就过（「这一趟对垃圾行做任何事都会毁掉救援」）——
		// 结果是垃圾行原地留下，而收件箱同步已经把救回来的信按新 UID 落了
		// 一行：一封信，两行，和 ERP 里点救援撞上的是同一个病，只是门在
		// 宿主那边。跟过去、用 mergeRepoint 收尾才是对的：占位的年轻副本
		// 会被合并掉，旧行带着历史坐到新位置。找不到新 UID 就先不动，下一
		// 轮对账重试——两行的状态多活二十秒，好过错删。
		case r.MessageID != "" && rescued[r.MessageID]:
			if uid, ok, err := s.mailbox.FindUIDByMessageID(ctx, acct, rescue, r.MessageID); err == nil && ok {
				// rescue 在唯一的调用处（JUNK 对账）是字面 "INBOX"——宿主名
				// 与本地逻辑名恰好同字。若将来有第二个 rescue 目的地，这里
				// 要把两种名字分开传。
				if err := s.mergeRepoint(ctx, tenantID, acct.AccountID,
					folder, r.ImapUid, rescue, int64(uid), r.MessageID); err != nil {
					s.log.Warn("could not follow a webmail rescue",
						"account", acct.AccountID, "message_id", r.MessageID, "err", err)
				}
			}
			saved++

		// 网页端标为垃圾：信在宿主的垃圾箱里了，垃圾箱同步很快会（或已经）
		// 把它按新 UID 另立一行。跟过去合并——行还是带着历史的那一行，只是
		// 搬进了 JUNK；年轻副本若已抢先落库，mergeRepoint 会清掉它。
		case folder == "INBOX" && r.MessageID != "" && inJunk[r.MessageID]:
			if uid, ok, err := s.mailbox.FindUIDByMessageID(ctx, acct, junkName, r.MessageID); err == nil && ok {
				if err := s.mergeRepoint(ctx, tenantID, acct.AccountID,
					folder, r.ImapUid, "JUNK", int64(uid), r.MessageID); err != nil {
					s.log.Warn("could not follow a webmail spam-marking",
						"account", acct.AccountID, "message_id", r.MessageID, "err", err)
				}
			}
			junked++

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
			// Already in our recycle bin, and now not even in the host's. The
			// host destroyed it — somebody emptied the trash, or the provider
			// aged it out — and a strict mirror destroys our copy too.
			//
			// This is the only outcome here that cannot be undone, so it is
			// the only one that refuses to run on the scan above. That scan
			// reads the newest departureScan messages of the trash, and a
			// mailbox whose bin is fuller than that would show perfectly
			// ordinary mail as "gone". A targeted SEARCH for this one
			// Message-ID answers the actual question instead of a proxy for
			// it. Two things therefore hold the trigger: no Message-ID means
			// no way to ask, and an error means no answer — neither is a yes.
			if r.DeletedAt.Valid {
				if r.MessageID == "" {
					continue
				}
				stillBinned, err := s.stillOnHost(ctx, acct, trash, r.HostFolder, r.HostUid, r.MessageID)
				if err != nil {
					s.log.Warn("could not confirm a host purge, so leaving the mail alone",
						"account", acct.AccountID, "message_id", r.MessageID, "err", err)
					continue
				}
				if stillBinned {
					continue
				}
				if err := s.purgeOne(ctx, tenantID, r.OwnerID, r.ID, r.RawKey); err != nil {
					s.log.Warn("could not mirror a host purge", "id", r.ID, "err", err)
					continue
				}
				purged++
				continue
			}
			// Not deleted yet, and nowhere we can see: expunged elsewhere, or
			// filed into a folder this ERP does not track. Treated as a
			// deletion, softly, and counted apart from the ones actually found
			// in the trash — somebody asking "why is my mail in the bin?"
			// deserves to be able to tell a confirmed deletion from an
			// inference.
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
	if deleted > 0 || archived > 0 || vanished > 0 || saved > 0 || junked > 0 || purged > 0 {
		s.log.Info("mail followed from the host",
			"account", acct.AccountID, "folder", folder,
			"in_trash", deleted, "archived", archived, "vanished", vanished,
			"rescued", saved, "junked", junked, "purged", purged)
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
	// Where the message sits on the host, under the host's own name for it.
	home, err := s.hostFolder(ctx, acct, row.Folder)
	if err != nil {
		return err
	}

	switch row.Flag {
	case flagTrash:
		trash, err := s.specialFolderOf(ctx, acct, "trash")
		if err != nil {
			return err
		}
		if row.Op == opAdd {
			// Straight out of the folder it is still sitting in.
			return s.moveAway(ctx, acct, row, home, trash)
		}
		return s.bringBack(ctx, acct, row, trash, home)

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
			return s.moveAway(ctx, acct, row, home, archive)
		}
		return s.bringBack(ctx, acct, row, archive, home)

	case flagNotJunk:
		junk, err := s.specialFolderOf(ctx, acct, "junk")
		if err != nil {
			return err
		}
		moved, err := s.mailbox.MoveMessages(ctx, acct, junk, []uint32{uint32(row.ImapUid)}, "INBOX")
		if err != nil {
			return err
		}
		// The message now lives in the inbox under a new UID. Following it is
		// not optional here: leave the row pointing at the old spam UID and
		// the next sync sees an unknown message in the inbox and files it a
		// second time. COPYUID says the new number outright; without it, search.
		if newUID, ok := moved[uint32(row.ImapUid)]; ok {
			return s.repointTo(ctx, row, "INBOX", int64(newUID))
		}
		return s.repoint(ctx, acct, row, "INBOX", "INBOX")

	case flagPurge:
		// Deleted mail is in the trash by now, and that is where it has to be
		// destroyed.
		trash, err := s.specialFolderOf(ctx, acct, "trash")
		if err != nil {
			return err
		}
		// 先用挪进回收站时记下来的 UID：有它就不用搜（263 不认搜索）。
		if uid, ok := s.knownHostUID(ctx, row, trash); ok {
			if err := s.mailbox.PurgeMessages(ctx, acct, trash, []uint32{uid}); err != nil {
				return err
			}
			s.log.Info("purged from the host for good",
				"account", acct.AccountID, "folder", trash, "uid", uid)
			return nil
		}
		uid, ok, err := s.mailbox.FindUIDByMessageID(ctx, acct, trash, row.MessageID)
		if err != nil {
			return err
		}
		if !ok {
			// Not in the trash. Usually that means somebody got there first —
			// emptied it by hand, or the provider aged it out — and there is
			// nothing left to do. But it can also mean the move that should
			// have put it there never happened, leaving the original sitting
			// in the folder it was deleted from. Saying "permanently deleted"
			// while a copy stays in the person's inbox is the one outcome this
			// operation must not produce, so look there before giving up.
			uid, ok, err = s.purgeStranded(ctx, acct, home, trash, row.MessageID)
			if err != nil {
				return err
			}
			if !ok {
				// Worth saying out loud rather than passing silently:
				// permanent deletion is the one operation nobody can check
				// afterwards, so "there was nothing there" and "it is gone
				// now" should not look the same in the log.
				s.log.Info("nothing left to purge on the host",
					"account", acct.AccountID, "message_id", row.MessageID)
				return nil
			}
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

// moveAway 把信挪出它的正位（进回收站、进归档），并记下它到了哪里。
//
// 记下来的那个 (host_folder, host_uid) 是之后一切操作的钥匙：彻底删除、恢复、
// 对账里问"还在回收站吗"，都直接按 UID 来，不再按 Message-ID 搜——263 不认
// 那种搜索。服务器没给 COPYUID 就不记，那些路径各自回退到搜索。
func (s *Service) moveAway(ctx context.Context, acct MailAccount, row store.ClaimFlagOpsRow, home, dest string) error {
	moved, err := s.mailbox.MoveMessages(ctx, acct, home, []uint32{uint32(row.ImapUid)}, dest)
	if err != nil {
		return err
	}
	newUID, ok := moved[uint32(row.ImapUid)]
	if !ok {
		return nil
	}
	if err := s.q.SetInboundHostLocation(ctx, store.SetInboundHostLocationParams{
		TenantID: row.TenantID, AccountID: row.AccountID,
		Folder: row.Folder, ImapUid: row.ImapUid,
		HostFolder: dest, HostUid: int64(newUID),
	}); err != nil {
		// 挪已经成功了；记不下位置只是让之后的操作退回搜索那条路。
		s.log.Warn("moved a mail but could not record where it went",
			"account", row.AccountID, "uid", row.ImapUid, "to", dest, "err", err)
	}
	return nil
}

// bringBack 把信从回收站/归档挪回正位。
//
// 知道它在那边的 UID 就直接挪，回来的新 UID 从 COPYUID 拿，行的身份当场改
// 过来（RepointInbound 顺手清掉 host_* 记录）。不知道就走老路：按 Message-ID
// 在那边找到它，挪回来，再按 Message-ID 找一次新号。
//
// 改行的身份不是可选项：行还指着它被删之前的号，下一次同步会在文件夹里看
// 见一封"不认识"的信，把恢复回来的这封再存一遍。
func (s *Service) bringBack(ctx context.Context, acct MailAccount, row store.ClaimFlagOpsRow, from, home string) error {
	if uid, ok := s.knownHostUID(ctx, row, from); ok {
		moved, err := s.mailbox.MoveMessages(ctx, acct, from, []uint32{uid}, home)
		if err != nil {
			return err
		}
		if newUID, ok := moved[uid]; ok {
			return s.repointTo(ctx, row, row.Folder, int64(newUID))
		}
		return s.repoint(ctx, acct, row, home, row.Folder)
	}
	if err := s.moveBack(ctx, acct, from, home, row.MessageID); err != nil {
		return err
	}
	return s.repoint(ctx, acct, row, home, row.Folder)
}

// knownHostUID 查这封信记下来的服务器位置；只有记录指向 expect 那个文件夹
// 才算数——记着"在归档里"的信，彻底删除时不能拿那个 UID 去回收站里删。
func (s *Service) knownHostUID(ctx context.Context, row store.ClaimFlagOpsRow, expect string) (uint32, bool) {
	r, err := s.q.GetInboundByFolderUID(ctx, store.GetInboundByFolderUIDParams{
		TenantID: row.TenantID, AccountID: row.AccountID,
		Folder: row.Folder, ImapUid: row.ImapUid,
	})
	if err != nil || r.HostUid <= 0 || r.HostFolder != expect {
		return 0, false
	}
	return uint32(r.HostUid), true
}

// repointTo 是 repoint 的"新号已知"版本：不搜，直接改行的身份。
func (s *Service) repointTo(ctx context.Context, row store.ClaimFlagOpsRow, erpFolder string, newUID int64) error {
	if err := s.mergeRepoint(ctx, row.TenantID, row.AccountID,
		row.Folder, row.ImapUid, erpFolder, newUID, row.MessageID); err != nil {
		s.log.Warn("could not repoint a moved mail", "id", row.ID, "err", err)
	}
	return nil
}

// hostFolder translates our name for a folder into the host's own.
//
// Ours is a fixed vocabulary — INBOX, JUNK, SENT — while every provider spells
// the last two differently ([Gmail]/Spam, 垃圾邮件, Junk E-mail). Sending our
// name to the host works only for the inbox, and fails silently enough to be
// missed: the folder simply cannot be selected and the write-back retries for
// ever.
func (s *Service) hostFolder(ctx context.Context, acct MailAccount, folder string) (string, error) {
	switch folder {
	case "", "INBOX":
		return "INBOX", nil
	case "JUNK":
		return s.specialFolderOf(ctx, acct, "junk")
	case "SENT":
		return s.specialFolderOf(ctx, acct, "sent")
	default:
		return folder, nil
	}
}

// purgeStranded routes a mail that never reached the host's trash through it,
// so that permanent deletion can finish the job, and reports the UID it landed
// under.
//
// Deleting straight out of the source folder would not do: on Gmail an expunge
// from a label only removes the label, so the mail would quietly survive in All
// Mail. It goes to the trash first, like any other deletion, and is destroyed
// from there.
func (s *Service) purgeStranded(ctx context.Context, acct MailAccount, home, trash, messageID string) (uint32, bool, error) {
	if home == "" || home == trash {
		return 0, false, nil
	}
	uid, ok, err := s.mailbox.FindUIDByMessageID(ctx, acct, home, messageID)
	if err != nil || !ok {
		return 0, false, err
	}
	s.log.Info("mail marked for permanent deletion never reached the host trash; moving it there first",
		"account", acct.AccountID, "folder", home, "uid", uid)
	moved, err := s.mailbox.MoveMessages(ctx, acct, home, []uint32{uid}, trash)
	if err != nil {
		return 0, false, err
	}
	// A move assigns a new UID in the destination, so the old one is no use
	// here. COPYUID names the new one; a host that does not say gets searched.
	if newUID, ok := moved[uid]; ok {
		return newUID, true, nil
	}
	return s.mailbox.FindUIDByMessageID(ctx, acct, trash, messageID)
}

// repoint updates our record of where a message lives after we moved it.
//
// Two folder names because the two vocabularies differ: hostFolder is where to
// look for the message ([Gmail]/Spam), erpFolder is what to write on the row
// (JUNK). Passing one for both is how a row ends up filed under a folder name
// no query will ever match.
//
// Best effort: a failure here costs a duplicate row on the next sync, not the
// move itself, and the move has already happened.
func (s *Service) repoint(ctx context.Context, acct MailAccount, row store.ClaimFlagOpsRow, hostFolder, erpFolder string) error {
	uid, ok, err := s.mailbox.FindUIDByMessageID(ctx, acct, hostFolder, row.MessageID)
	if err != nil || !ok {
		s.log.Warn("moved a mail but could not find its new UID",
			"account", acct.AccountID, "to", hostFolder, "err", err)
		return nil
	}
	if err := s.mergeRepoint(ctx, row.TenantID, row.AccountID,
		row.Folder, row.ImapUid, erpFolder, int64(uid), row.MessageID); err != nil {
		s.log.Warn("could not repoint a moved mail", "id", row.ID, "err", err)
	}
	return nil
}

// mergeRepoint 把挪过的信的本地行改指向它在新文件夹里的新 UID——必要时先清场。
//
// 清场针对的是一场几乎必输的赛跑：MoveMessages 一执行，目的文件夹的 IDLE
// 立刻收到推送，同步抢在这里之前把挪过去的信当新邮件下载了一遍。于是目的
// 位置已经被同一封信的年轻副本占住，原来的 RepointInbound 撞上唯一约束、
// 记条 warn 就放弃——旧行留在旧文件夹，一封信从此两行。RepointInbound 的
// 注释早写着「one mail, two rows」是它要防的事，但它防不过 IDLE 的速度。
//
// 合并的方向是**保旧弃新**：旧行带着阅读状态、星标和被引用的 id（智能转换
// 任务等都指着它）；年轻副本是几秒前才落库的，什么都不带。清场走 purgeOne，
// 行、附件、缓存图片、原始邮件对象一起走，不给存储留孤儿。
//
// 只有占位者与被挪的信是同一个 Message-ID 才清（两者都非空）。占位者是别的
// 信意味着 UID 语义出了更大的问题，这里不该猜——保持原来的行为：改不动，
// 记 warn，两行都留着等人看。
func (s *Service) mergeRepoint(ctx context.Context, tenantID, accountID int64, oldFolder string, oldUID int64, newFolder string, newUID int64, messageID string) error {
	if oldFolder == newFolder && oldUID == newUID {
		return nil // 挪了个寂寞：已经在该在的位置上
	}
	occ, err := s.q.GetInboundByFolderUID(ctx, store.GetInboundByFolderUIDParams{
		TenantID: tenantID, AccountID: accountID, Folder: newFolder, ImapUid: newUID,
	})
	switch {
	case err == nil && messageID != "" && occ.MessageID == messageID:
		// 同一封信的年轻副本。清掉它，位置让给带着历史的旧行。
		//
		// 两步走的是现成的销毁路径：purgeOne 只肯销毁回收站里的行（deleted_at
		// 非空是它的守卫，防误删），所以先用 MirrorHostDelete 把副本软删进
		// 回收站，再销毁。不为这一处发明绕开守卫的新删法。
		if err := s.q.MirrorHostDelete(ctx, store.MirrorHostDeleteParams{
			TenantID: tenantID, AccountID: accountID, Folder: newFolder, ImapUid: newUID,
		}); err != nil {
			return fmt.Errorf("bin the freshly synced duplicate: %w", err)
		}
		if err := s.purgeOne(ctx, tenantID, occ.OwnerID, occ.ID, occ.RawKey); err != nil {
			return fmt.Errorf("clear the freshly synced duplicate: %w", err)
		}
		s.log.Info("cleared a duplicate the sync raced in ahead of a move",
			"account", accountID, "folder", newFolder, "uid", newUID)
	case err == nil:
		// 位置被一封不同的信占着——不猜，留给人看。
		return fmt.Errorf("destination %s/%d is held by a different message", newFolder, newUID)
	case !errors.Is(err, pgx.ErrNoRows):
		return err
	}
	return s.q.RepointInbound(ctx, store.RepointInboundParams{
		TenantID: tenantID, AccountID: accountID,
		OldFolder: oldFolder, OldUid: oldUID,
		NewFolder: newFolder, NewUid: newUID,
	})
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
	_, err = s.mailbox.MoveMessages(ctx, acct, from, []uint32{uid}, to)
	return err
}

// stillOnHost 问"这封信还在那个文件夹里吗"。记了 UID 就按 UID 查（一次
// FETCH），没记就按 Message-ID 搜（263 不认）。
func (s *Service) stillOnHost(ctx context.Context, acct MailAccount, folder, hostFolder string, hostUID int64, messageID string) (bool, error) {
	if hostUID > 0 && hostFolder == folder {
		live, err := s.mailbox.FetchFlags(ctx, acct, folder, []uint32{uint32(hostUID)})
		if err != nil {
			return false, err
		}
		_, there := live[uint32(hostUID)]
		return there, nil
	}
	if messageID == "" {
		return false, nil
	}
	_, there, err := s.mailbox.FindUIDByMessageID(ctx, acct, folder, messageID)
	return there, err
}

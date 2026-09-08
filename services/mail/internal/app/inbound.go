package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sgao19/erp-go/pkg/livefeed"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// RawMessage is one message as the server holds it.
type RawMessage struct {
	UID uint32
	// UIDValidity of the folder this UID was read from. Carried alongside the
	// UID because a UID means nothing without it: the pair is what identifies
	// a message, and the host is entitled to renumber everything by changing
	// the validity.
	UIDValidity  uint32
	Raw          []byte
	InternalDate time.Time
	// Seen is the host's own read flag. Backfilled history the person read
	// years ago in another client must not arrive here as five hundred
	// unread messages — that would bury the badge's actual signal.
	Seen bool
}

// FetchResult is one pass over a mailbox.
type FetchResult struct {
	UIDValidity uint32
	Messages    []RawMessage
}

// Mailbox is the inbound side of a mail host.
// FolderStatus is what a cheap poll of a folder returns.
type FolderStatus struct {
	// UIDNext 是服务器下一封信会拿到的编号。和我们存的 last_uid 比：
	// UIDNext > last_uid+1 就说明有没取过的信。精确，不是估计——UID 在一个
	// UIDVALIDITY 里单调递增，这正是它存在的意义。
	UIDNext uint32
	// Unseen 是服务器数的未读数。**它不上屏**：角标一律用我们自己库里的数
	// （见 CountUnreadByMailbox），两边口径不同（服务器不知道我们归档过什么），
	// 混着用的症状是「角标写着 3，切过去一封都没有」。
	//
	// 它在这里只有一个用途：和我们库里的数不一致时，说明有人在别的客户端
	// 上读过或删过信，那也是一次值得全量同步的变化。
	Unseen uint32
	// UIDValidity 是这个文件夹此刻的编号世代。和 mail_sync_state 里存的比：
	// 不一样，我们手里所有 UID 都作废——它们指向别的信，或者什么都不指。
	// 写回操作在判断「这封信还在不在」之前必须先看它，不然换代之后每个
	// 旧 UID 都"不在"，会把一堆没做成的删除当成已完成静默作废掉。
	UIDValidity uint32
}

type Mailbox interface {
	// Fetch returns new mail: everything above sinceUID, capped at limit.
	Fetch(ctx context.Context, acct MailAccount, folder string, sinceUID uint32, limit uint32) (FetchResult, error)
	// FetchBelow returns history: the newest `limit` messages below belowUID.
	FetchBelow(ctx context.Context, acct MailAccount, folder string, belowUID uint32, limit uint32) (FetchResult, error)
	// FetchByUIDs returns exactly the messages named. The UIDs must be
	// current-generation — in practice they come from a Message-ID search
	// moments earlier over the same connection pool.
	FetchByUIDs(ctx context.Context, acct MailAccount, folder string, uids []uint32) (FetchResult, error)
	// ListFolders 列出服务器上所有文件夹的名字（不含特殊属性的判断，那是
	// SentFolder 那一组的事）。
	ListFolders(ctx context.Context, acct MailAccount) ([]HostFolder, error)
	// CreateFolder / RenameFolder / DeleteFolder 在服务器上真的建、改、删一个
	// 文件夹。名字是人写的（中文也行），UTF-7 编码由适配器负责。
	CreateFolder(ctx context.Context, acct MailAccount, name string) error
	RenameFolder(ctx context.Context, acct MailAccount, oldName, newName string) error
	DeleteFolder(ctx context.Context, acct MailAccount, name string) error
	// FolderStatus asks the host two numbers about a folder and nothing else:
	// how far its UIDs have advanced, and how many messages are unread.
	//
	// 它存在的理由是「这个箱里有没有我们还没取过的信」**不值得一次全量
	// 同步**。一人多箱之后每轮把每个箱都拉一遍摊不开（600 个箱、8 个
	// worker、两分钟一轮 = 每个箱 1.6 秒），而绝大多数箱这一轮什么都没发生。
	//
	// IMAP 的 STATUS 一条命令就回这两个数：不打开信箱、不动这条连接当前
	// 选中的文件夹、不传任何正文。
	FolderStatus(ctx context.Context, acct MailAccount, folder string) (FolderStatus, error)
	// SentFolder names the folder the host keeps sent mail in.
	SentFolder(ctx context.Context, acct MailAccount) (string, error)
	// JunkFolder names the folder the host files spam into.
	JunkFolder(ctx context.Context, acct MailAccount) (string, error)
	// VerifyLogin authenticates and disconnects: it proves the credentials
	// work today, and nothing else.
	VerifyLogin(ctx context.Context, acct MailAccount) error
	// SetFlags publishes a flag change to the host.
	SetFlags(ctx context.Context, acct MailAccount, folder string, uids []uint32, flag string, add bool) error
	// TrashFolder names the folder deleted mail goes to.
	TrashFolder(ctx context.Context, acct MailAccount) (string, error)
	// ArchiveFolder names where archived mail goes, or "" when this host has
	// no such place — a plain IMAP server has no archive concept at all.
	ArchiveFolder(ctx context.Context, acct MailAccount) (string, error)
	// MoveMessages moves mail between folders on the host.
	// MoveMessages 把信挪到另一个文件夹，返回「旧 UID → 新 UID」。
	//
	// 新 UID 来自服务器应答里的 COPYUID（UIDPLUS 扩展，我们接的每一家都
	// 支持）。拿到它，之后彻底删除、恢复就直接按 UID 操作，不用再按
	// Message-ID 搜——263 不认那种搜索。服务器没给时返回空 map，调用方回退
	// 到搜索。
	MoveMessages(ctx context.Context, acct MailAccount, from string, uids []uint32, to string) (map[uint32]uint32, error)
	// FindUIDByMessageID follows a message that has moved: its UID changed,
	// its Message-ID did not.
	FindUIDByMessageID(ctx context.Context, acct MailAccount, folder, messageID string) (uint32, bool, error)
	// FindUIDsByMessageIDs answers the same question for many messages over
	// one connection. Emptying a trash asks it once per mail, and a fresh
	// authenticated connection each time is what a host reads as abuse.
	FindUIDsByMessageIDs(ctx context.Context, acct MailAccount, folder string, messageIDs []string) (map[string]uint32, error)
	// PurgeMessages deletes mail from the host for good.
	PurgeMessages(ctx context.Context, acct MailAccount, folder string, uids []uint32) error
	// FetchFlags reads back what the host believes, so somebody else's
	// changes reach the ERP too. Bounded by the UIDs it is given.
	FetchFlags(ctx context.Context, acct MailAccount, folder string, uids []uint32) (map[uint32]MessageFlags, error)
	// SearchFlagged names every starred message in a folder, at any age.
	// FetchFlags cannot: it only answers about the UIDs handed to it, which
	// in practice is the newest few hundred.
	SearchFlagged(ctx context.Context, acct MailAccount, folder string) ([]uint32, error)
	// RecentMessageIDs names the newest messages of a folder, for working out
	// where mail went once it stops appearing in the inbox.
	RecentMessageIDs(ctx context.Context, acct MailAccount, folder string, limit uint32) (map[string]bool, error)
	// AppendMessage files a message we already sent into a folder on the
	// host — the one write in this interface that creates a message rather
	// than moving or flagging one. See saveSentCopy for why it exists.
	AppendMessage(ctx context.Context, acct MailAccount, folder string, raw []byte, at time.Time) error
}

// MessageFlags is the host's view of one message.
type MessageFlags struct {
	Seen    bool
	Flagged bool
}

// UseMailbox installs the inbound adapter. Same two-step wiring as the
// sender, and for the same reason: the adapter needs the service to resolve
// credentials.
func (s *Service) UseMailbox(m Mailbox) { s.mailbox = m }

// SyncConfig tunes the inbound poller.
type SyncConfig struct {
	TenantID int64
	Folder   string
	Interval time.Duration
	// Ceiling on one pass, so a mailbox with ten years of history does not
	// take the service down on its first run.
	BatchSize uint32
	// How much history to hold per folder. The backfill walks the past one
	// batch per pass until this many messages are stored; 0 means the default.
	HistoryCap int64
	// How many mailboxes may sync at once. See syncfleet.go.
	Concurrency int
	// Where a recipient's mail client reaches us — the same address the
	// tracking pixel is built from. The image cache needs it for one reason
	// only: to recognise our own pixel when a customer quotes our mail back
	// at us, and refuse to fetch it. See imagecache.go.
	PublicBaseURL string
	// ActiveWindow 是「多久没人看就算没人看了」。
	//
	// 落在窗口里的箱每轮全量同步；其余的只问一次轻状态（见 FolderStatus）。
	// 分档只改延迟不改对错：轻状态看见有没取过的信就把那个箱提上来。
	ActiveWindow time.Duration
	// StatusEvery 是没人看的箱多久问一次轻状态。它就是那类箱的收信延迟上限。
	StatusEvery time.Duration
	// StatusBudget 是一轮里最多问多少个轻状态。
	//
	// 上限而不是「全问」：600 个箱一起到点会在一轮里堆出一个尖峰，把全量
	// 那一档挤掉——而那一档才是有人正在等的。到不了的下一轮再说，
	// status_checked_at 最旧的排最前，所以没有箱会被永远跳过。
	StatusBudget int
}

func (c SyncConfig) withDefaults() SyncConfig {
	// TenantID 刻意不再兜底成 1。
	//
	// 它以前是 1，而 cmd/main.go 从来没传过——于是三个后台循环（收信轮询、IDLE
	// 长连接、发信 worker）全都只服务第一家公司。第二家公司的员工把邮箱绑好、
	// 授权码填对、页面上一切正常，信却永远不会来，而且**不报任何错**：循环按
	// 名单干活，名单里没有就等于不存在。
	//
	// 现在名单由 tenantsToServe 每轮现查。这里留 0 是有意的：0 表示「还没说是
	// 哪家」，让漏传变成一次空转，而不是安静地服务错的那家。
	if c.Folder == "" {
		c.Folder = "INBOX"
	}
	if c.Interval <= 0 {
		c.Interval = 2 * time.Minute
	}
	if c.BatchSize == 0 {
		c.BatchSize = 50
	}
	if c.HistoryCap <= 0 {
		c.HistoryCap = 500
	}
	if c.Concurrency <= 0 {
		c.Concurrency = 8
	}
	if c.ActiveWindow <= 0 {
		// 半小时。比「页面开着」宽得多是有意的：人去开个会回来，箱不该在
		// 这期间掉档，因为掉档意味着回来时看到的是十分钟前的收件箱。
		c.ActiveWindow = 30 * time.Minute
	}
	if c.StatusEvery <= 0 {
		// 十分钟，也就是没人看的箱的收信延迟上限。这个数字的另一面是成本：
		// 600 个箱、十分钟摊一遍、每次一条 STATUS，大约占 8 个 worker 的
		// 一成半——剩下的都留给有人在等的那一档。
		c.StatusEvery = 10 * time.Minute
	}
	if c.StatusBudget <= 0 {
		// 一轮 150 个。按两分钟一轮、十分钟一遍算，600 个箱刚好摊得开
		// （600 / 5 = 120），留一点余量给新绑的和上一轮没轮到的。
		c.StatusBudget = 150
	}
	return c
}

// RunInboundSync polls every configured mailbox for ever.
//
// Polling rather than pushing because a mail host offers nothing else. The
// interval is the honest cost of that: a reply is visible within one cycle,
// not instantly, and pretending otherwise would mean holding an IDLE
// connection open per employee.
func (s *Service) RunInboundSync(ctx context.Context, cfg SyncConfig) {
	cfg = cfg.withDefaults()
	if s.mailbox == nil {
		s.log.Info("inbound sync not started — no mailbox adapter configured")
		return
	}
	s.log.Info("inbound sync started", "folder", cfg.Folder, "every", cfg.Interval)

	t := time.NewTicker(cfg.Interval)
	defer t.Stop()
	for {
		// 每家公司各过一遍。并发上限是**全局的**（fleet 只有一个），所以公司多
		// 起来不会变成成倍的连接同时打向邮件服务商——受不了的是对面，不是我们。
		for _, tenantID := range s.tenantsToServe(ctx) {
			pass := cfg
			pass.TenantID = tenantID
			s.syncAllMailboxes(ctx, pass)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// checkMailboxStatus 问一个没人在看的信箱：有没有我们还没取过的信。
//
// 一条 IMAP STATUS。不打开信箱、不拉正文，也**不动屏幕上的任何数字**——
// 角标一律来自我们自己的库（CountUnreadByMailbox），两边口径不同（服务器
// 不知道我们归档过什么），混着用的症状是「角标写着 3，切过去一封都没有」。
//
// 它只决定一件事：这个箱这一轮要不要被提上来全量同步。
//
//	· UIDNext 超过我们存的 last_uid + 1 —— 有没取过的信。精确，不是估计。
//	· Unseen 和我们库里的数对不上 —— 有人在别的客户端上读过或删过，
//	  我们的已读状态该跟一次。
//
// 提上来之后走的是和有人在看的箱**同一条**全量同步，所以「怎么收信」只有
// 一套代码；这里只回答「要不要收」。
func (s *Service) checkMailboxStatus(ctx context.Context, cfg SyncConfig, accountID int64) {
	acct, err := s.ForAccount(ctx, cfg.TenantID, accountID)
	if err != nil {
		// 凭据坏了在这里只记一句：全量那条路上有 RecordFailure 把它写到
		// 账号行上、让设置页说得出话，重复一遍没有新信息。
		s.log.Warn("status check could not resolve the mailbox", "account", accountID, "err", err)
		return
	}
	st, err := s.mailbox.FolderStatus(ctx, acct, cfg.Folder)
	if err != nil {
		s.log.Warn("status check failed", "account", accountID, "err", err)
		// 时间戳照记：问失败了也别在下一轮立刻重问，那会让一个连不上的箱
		// 每两分钟占一个 worker。等它下一次到点。
		s.markStatusChecked(ctx, cfg.TenantID, accountID)
		return
	}
	s.markStatusChecked(ctx, cfg.TenantID, accountID)

	if !s.worthAFullSync(ctx, cfg, acct, st) {
		return
	}
	if n, err := s.SyncMailboxIfDue(ctx, cfg, accountID); err != nil {
		s.log.Warn("promoted mailbox sync failed", "account", accountID, "err", err)
	} else if n > 0 {
		s.log.Info("idle mailbox had news", "account", accountID, "new", n)
	}
}

// worthAFullSync 判断一次轻状态的结果值不值得把这个箱提上来全量同步。
//
// 判不准时一律回 true。少收一封信和多同步一次不是一个量级的错：前者是
// 客户的询价没到，后者是多花几秒。
func (s *Service) worthAFullSync(ctx context.Context, cfg SyncConfig, acct MailAccount, st FolderStatus) bool {
	have, err := s.q.HighestSyncedUID(ctx, store.HighestSyncedUIDParams{
		TenantID: cfg.TenantID, AccountID: acct.AccountID, Folder: cfg.Folder,
	})
	if err != nil {
		s.log.Warn("could not read the sync cursor; syncing anyway",
			"account", acct.AccountID, "err", err)
		return true
	}
	// 服务器没给 UIDNEXT——STATUS 的返回项不是每台主机都齐全。这时**什么都
	// 判不出来**，只能全量同步一次。
	//
	// 这一条不能省。省了的话，一台不回 UIDNEXT 的主机上所有没人看的箱都会
	// 被判成「没有新信」，于是永远不被提上来——症状是那些箱安静地不再收信，
	// 而日志里一个字都没有。分档能成立的全部依据就是「判不准就同步」。
	if st.UIDNext == 0 {
		return true
	}
	// UIDNext 是「下一封会拿到的号」，所以「已取到 have」意味着下一封应该
	// 正好是 have+1。大于它就是中间来过我们没取的信。
	if int64(st.UIDNext) > have+1 {
		return true
	}
	// 没有新信，但未读数对不上：有人在别的客户端上读了或删了。跟一次，
	// 好让这边的已读状态和角标不再骗人。
	//
	// owner 从账号行上来（ForAccount 已经取过），不另查一次——而且它必须是
	// 这个箱的主人，不是随便一个人：email_inbound 是按 owner_id 存的。
	id := acct.AccountID
	unread, err := s.q.CountUnread(ctx, store.CountUnreadParams{
		TenantID: cfg.TenantID, OwnerID: acct.EmployeeID, AccountID: &id,
	})
	if err != nil {
		return true
	}
	return int64(st.Unseen) != unread
}

func (s *Service) markStatusChecked(ctx context.Context, tenantID, accountID int64) {
	if err := s.q.MarkStatusChecked(ctx, store.MarkStatusCheckedParams{
		TenantID: tenantID, ID: accountID,
	}); err != nil {
		s.log.Warn("could not record the status check", "account", accountID, "err", err)
	}
}

// tenantsToServe 是本轮要处理的公司名单：有活跃邮箱的那些。
//
// 每轮现查，不缓存——新开的公司下一轮就被发现，不用等进程重启。
//
// 取不到就返回空，让这一轮空转，**不回落到「第一家公司」**。回落看着更“健壮”，
// 实际是把「暂时不知道有哪些公司」偷换成「就服务这一家」，而那正是这套代码原来
// 的毛病：错得安静，没人发现。下一轮自然会重试。
func (s *Service) tenantsToServe(ctx context.Context) []int64 {
	ids, err := s.q.ListTenantsWithMailboxes(ctx)
	if err != nil {
		s.log.Error("could not list tenants to serve; skipping this pass", "err", err)
		return nil
	}
	return ids
}

// syncAllMailboxes 走完一家公司的一轮。
//
// **两档，不是一遍。**
//
// 从前每轮把每个活跃信箱都全量同步一次。一人一箱的年代那没问题；一人两箱
// 之后，300 人的公司就是 600 个箱，8 个 worker、两分钟一轮，平均每个箱只剩
// 1.6 秒——跨太平洋一次握手就超了。超了的样子不是报错，是所有人的信一起
// 晚到，而且越积越晚。
//
// 现在：
//
//	· 有人在看的箱（last_read_at 新鲜）——照旧全量同步。这一档大约 50 个，
//	  每个箱有 19 秒，比原来宽得多。
//	· 其余——只问一条 STATUS，十分钟摊一遍。看见有没取过的信就**当场提上来**
//	  全量同步。
//
// 最后那一句是这个设计的全部依据：**分档改的是延迟，不是对错**。没人看的箱
// 来了新信，最坏晚十分钟，不会收不到。
func (s *Service) syncAllMailboxes(ctx context.Context, cfg SyncConfig) {
	accounts, err := s.q.ListActiveMailAccounts(ctx, store.ListActiveMailAccountsParams{
		TenantID: cfg.TenantID, ActiveSeconds: int32(cfg.ActiveWindow.Seconds()),
	})
	if err != nil {
		s.log.Error("could not list mailboxes to sync", "err", err)
		return
	}
	// 轻状态那一档先挑出来：它要在同一个 fleet 上跑，和全量那一档共用并发
	// 上限——受不了并发的是邮件服务商，不是我们，所以上限必须是全局的。
	due, err := s.q.ListMailboxesDueForStatus(ctx, store.ListMailboxesDueForStatusParams{
		TenantID:      cfg.TenantID,
		ActiveSeconds: int32(cfg.ActiveWindow.Seconds()),
		StatusSeconds: int32(cfg.StatusEvery.Seconds()),
		RowLimit:      int32(cfg.StatusBudget),
	})
	if err != nil {
		// 轻状态挑不出来不该拖累全量那一档：有人正等着的那些箱照收。
		s.log.Error("could not list mailboxes due for a status check", "err", err)
		due = nil
	}
	// Fanned out rather than walked. Sequentially, one mailbox that hangs
	// holds up every mailbox behind it — and a mailbox whose host accepts the
	// connection and then says nothing holds them up for the whole dial
	// timeout, every cycle, for as long as nobody fixes it. The fleet bounds
	// how many run at once, so a stuck mailbox costs one worker.
	var wg sync.WaitGroup
	for _, a := range accounts {
		select {
		case <-ctx.Done():
			return
		default:
		}
		wg.Add(1)
		go func(accountID int64) {
			defer wg.Done()
			// One mailbox failing must not stop the rest: a single employee's
			// expired authorisation code should not stop the whole company
			// receiving mail.
			if n, err := s.SyncMailboxIfDue(ctx, cfg, accountID); err != nil {
				s.log.Warn("mailbox sync failed", "account", accountID, "err", err)
			} else if n > 0 {
				s.log.Info("mailbox synced", "account", accountID, "new", n)
			}
		}(a.ID)
	}
	// 轻状态那一档挂在同一个 WaitGroup 上，所以「一轮跑完再开下一轮」这条
	// 规矩对两档一起成立。
	for _, a := range due {
		select {
		case <-ctx.Done():
			return
		default:
		}
		wg.Add(1)
		go func(accountID int64) {
			defer wg.Done()
			s.checkMailboxStatus(ctx, cfg, accountID)
		}(a.ID)
	}
	// Waited on, so one pass finishes before the next tick starts it again.
	// Without this a slow cycle would overlap the next and the mailboxes at
	// the end of the list would be synced by two passes at once.
	wg.Wait()

	// Backing off makes failure quiet, and quiet failure is what this service
	// has been bitten by before: a mailbox that stops receiving while the page
	// goes on showing the last successful sync. The count is said out loud
	// once per pass so "mail is not arriving" is visible in the log rather
	// than only in the absence of anything.
	if n := s.fleet(cfg.Concurrency).health.failingCount(); n > 0 {
		s.log.Warn("mailboxes are failing and being retried less often",
			"failing", n, "of", len(accounts))
	}
}

// SyncMailbox pulls one mailbox — new mail first, then a slice of history —
// and returns how many new INBOX messages it stored.
func (s *Service) SyncMailbox(ctx context.Context, cfg SyncConfig, accountID int64) (int, error) {
	cfg = cfg.withDefaults()
	// Everything that syncs a mailbox comes through here — the poller, the
	// idle watcher and a person clicking 立即收信 — so this is where the
	// fleet's two rules apply: a bounded number at once, and never the same
	// mailbox twice over.
	return s.fleet(cfg.Concurrency).do(ctx, accountID, func() (int, error) {
		return s.syncMailboxNow(ctx, cfg, accountID, nil)
	})
}

// inboxTail is how long the rest of an interactive pass may keep running once
// the caller has been answered. Generous, because it is doing real work that
// nobody is waiting on; bounded, because a mail host that accepts a connection
// and then says nothing must not leave a goroutine and a mailbox slot held
// for ever.
const inboxTail = 5 * time.Minute

// interactiveWait is how long 立即收信 will hold the HTTP request open before
// answering "还在收".
//
// The inbox leg is not instant on a mailbox that has never been synced: nine
// thousand messages arrive in batches, each one a download and an upload. The
// caller is a browser behind nginx, whose patience is 60 seconds and whose way
// of running out is a 504 — an error page for something that is not an error
// and that is, at that very moment, working.
//
// So the wait is bounded well under that and the tail carries on regardless.
// Twenty seconds is chosen from the other end too: past it, a person who
// clicked a button has stopped believing it did anything.
const interactiveWait = 20 * time.Second

// awaitInbox waits for the inbox leg of an interactive pass.
//
// Three outcomes, and the middle one is the point: answered, still running, or
// the caller gave up. "Still running" is not an error and must not be dressed
// as one — the mail is on its way in and saying so is the honest report.
//
// Shared with the tests rather than mirrored by them: a test that reimplements
// the control flow it is meant to pin will agree with itself forever.
func awaitInbox(ctx context.Context, inbox <-chan syncOutcome, wait time.Duration) (int, bool, error) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case r := <-inbox:
		return r.n, false, r.err
	case <-timer.C:
		return 0, true, nil
	case <-ctx.Done():
		// The person navigated away. The pass carries on regardless — the
		// mail is worth having whether or not anyone is still watching.
		return 0, false, ctx.Err()
	}
}

type syncOutcome struct {
	n   int
	err error
}

// SyncMailboxInteractive is 立即收信: the same pass as SyncMailbox, but it
// answers as soon as the inbox is in and leaves the rest running.
//
// A full pass is six folder-level round trips - INBOX, SENT and JUNK synced,
// then read state reconciled back over all three - and it used to run to the
// end before the button stopped spinning. Five sixths of that wait is spent on
// work the person did not ask for: they clicked 立即收信 to see whether the
// customer had replied, and the reply is in the first sixth.
//
// The tail stays inside the fleet call rather than being spawned out of it,
// which matters: the fleet is what stops the same mailbox being synced twice
// over, and a tail running outside it would let a second click open a second
// IMAP session on an account that already has one.
// Returns (收到几封, 是否仍在后台继续, 错误).
func (s *Service) SyncMailboxInteractive(ctx context.Context, cfg SyncConfig, accountID int64) (int, bool, error) {
	cfg = cfg.withDefaults()
	inbox := make(chan syncOutcome, 1)

	// Detached from the request: the tail outlives the HTTP call that started
	// it, and cancelling that call must not abandon a half-finished pass.
	tail, cancel := context.WithTimeout(context.WithoutCancel(ctx), inboxTail)
	go func() {
		defer cancel()
		n, err := s.fleet(cfg.Concurrency).do(tail, accountID, func() (int, error) {
			return s.syncMailboxNow(tail, cfg, accountID, inbox)
		})
		// Nobody signalled: either the inbox leg failed, or the fleet joined
		// this caller onto a pass that was already running and belongs to
		// somebody else. Deliver the outcome so the caller is not left
		// waiting on a channel that will never be written.
		select {
		case inbox <- syncOutcome{n, err}:
		default:
		}
	}()

	return awaitInbox(ctx, inbox, interactiveWait)
}

// SyncMailboxIfDue is the poller's entry point: it skips a mailbox that failed
// recently rather than spending a worker rediscovering the same failure.
//
// The distinction from SyncMailbox matters. A person clicking 立即收信 has
// asked, and gets an attempt whatever the mailbox's history — they may well be
// clicking *because* they just fixed it. The poller has not been asked by
// anybody, so it is the one that should hold back.
func (s *Service) SyncMailboxIfDue(ctx context.Context, cfg SyncConfig, accountID int64) (int, error) {
	cfg = cfg.withDefaults()
	f := s.fleet(cfg.Concurrency)
	due, failing := f.health.dueAt(accountID, time.Now())
	if !due {
		return 0, nil
	}
	if failing {
		// Known-bad mailboxes share a small reserved part of the fleet, so a
		// wave of them coming due at once cannot fill it and leave working
		// mailboxes queueing behind mailboxes that do not work.
		release, ok := f.health.holdSick()
		if !ok {
			return 0, nil
		}
		defer release()
	}
	n, err := s.SyncMailbox(ctx, cfg, accountID)
	f.health.record(accountID, time.Now(), err)
	return n, err
}

// syncMailboxNow runs a full pass. inboxDone, when non-nil, receives the inbox
// leg's result the moment it is stored and committed, so an interactive caller
// can be answered without waiting for the five round trips behind it.
func (s *Service) syncMailboxNow(ctx context.Context, cfg SyncConfig, accountID int64, inboxDone chan<- syncOutcome) (int, error) {
	// Buffered by every caller, so this never blocks the pass on a reader
	// that has already gone away.
	signal := func(n int, err error) {
		if inboxDone == nil {
			return
		}
		select {
		case inboxDone <- syncOutcome{n, err}:
		default:
		}
	}

	acct, err := s.ForAccount(ctx, cfg.TenantID, accountID)
	if err != nil {
		// 只记凭据类的失败。ForAccount 也会因为"已解绑 / 已暂停"而失败，那些
		// 不该往一个不存在或休眠的账号上写错误；而 Google 授权被撤销（换过
		// 密码、在安全页里撤了）正是这里失败，不记的话页面永远不会给那颗
		// "重新登录"——它是唯一修得好这件事的按钮。
		if IsCredentialRejected(err) {
			s.RecordFailure(ctx, cfg.TenantID, accountID, err.Error(), true)
		}
		return 0, err
	}

	newInbox, err := s.syncFolder(ctx, cfg, acct, "INBOX", "INBOX")
	if err != nil {
		_ = s.q.MarkSyncFailed(ctx, store.MarkSyncFailedParams{
			TenantID: cfg.TenantID, AccountID: acct.AccountID, Folder: "INBOX",
			LastError: err.Error(),
		})
		// Also onto the account, which is what the mailbox page reads. A
		// revoked authorisation used to fail here every two minutes for as
		// long as it took somebody to notice, while the page went on showing
		// the last successful sync as though it were current. Silence is the
		// bug: the mailbox has to be able to say it is not receiving.
		s.RecordFailure(ctx, cfg.TenantID, acct.AccountID, err.Error(), IsCredentialRejected(err))
		return 0, err
	}
	// Cleared on the way back up, so a recovered mailbox stops complaining
	// without anybody having to sign in again.
	s.clearFailure(ctx, cfg.TenantID, acct.AccountID)

	// The inbox is in. Everything below is worth doing and nobody is waiting
	// on it, so an interactive caller is answered here rather than at the end.
	signal(newInbox, nil)

	// Sent history rides along on the same pass. A failure here is logged and
	// does not fail the sync: the inbox is what somebody is waiting on.
	if actual, err := s.specialFolderOf(ctx, acct, "sent"); err != nil {
		s.log.Warn("could not locate the sent folder", "account", acct.AccountID, "err", err)
	} else if _, err := s.syncFolder(ctx, cfg, acct, "SENT", actual); err != nil {
		s.log.Warn("sent-folder sync failed", "account", acct.AccountID, "err", err)
	}

	// The junk folder too — read-only safety net for the false positive: the
	// customer inquiry the host wrongly filed as spam would otherwise be
	// invisible to somebody using this as their only client. Shallower
	// history than the inbox: old spam is the least valuable mail there is.
	jcfg := cfg
	if jcfg.HistoryCap > 100 {
		jcfg.HistoryCap = 100
	}
	if actual, err := s.specialFolderOf(ctx, acct, "junk"); err != nil {
		s.log.Warn("could not locate the junk folder", "account", acct.AccountID, "err", err)
	} else if _, err := s.syncFolder(ctx, jcfg, acct, "JUNK", actual); err != nil {
		s.log.Warn("junk-folder sync failed", "account", acct.AccountID, "err", err)
	}

	// 自建文件夹和服务器自带、ERP 也认得是真文件夹的那些（163 的病毒文件夹、
	// QQ 的其他文件夹），内容一起收进来。员工在 Foxmail 里把信拖进「重要客户」
	// 之后 ERP 也看得到，就是靠这一段——那条 v1 边界到此为止。
	//
	// 不收的：归档和回收站（ERP 的归档/删除是"行留在收件箱加个标记、服务器
	// 那份挪走"，收进来同一封信会多出一行）、草稿箱（下一期）、虚拟文件夹
	// （Gmail 的标签，收进来会把每封信存好几遍）。判断在 syncableRole。
	s.syncExtraFolders(ctx, cfg, acct)

	// The host's own read state, taken back over the newest slice of the
	// inbox. This is the half of two-way sync that carries somebody else's
	// Gmail session into the ERP; it no-ops while local changes are still
	// queued, so it can never overwrite one on its way up.
	if err := s.ReconcileFlags(ctx, cfg.TenantID, acct, "INBOX", "INBOX", ""); err != nil {
		s.log.Warn("could not reconcile the inbox", "account", acct.AccountID, "err", err)
	}
	// The junk folder, with the inbox named as its rescue: mail leaves spam
	// either because somebody deleted it or because somebody called it "not
	// spam", and only the second lands in the inbox. Departures here used to
	// be ignored altogether to protect the rescue, which also meant deleting a
	// spam in Gmail reached nothing at all.
	if actual, err := s.specialFolderOf(ctx, acct, "junk"); err == nil {
		if err := s.ReconcileFlags(ctx, cfg.TenantID, acct, "JUNK", actual, "INBOX"); err != nil {
			s.log.Warn("could not reconcile the junk folder", "account", acct.AccountID, "err", err)
		}
	}
	// And the sent folder, departures included. This was the one remaining
	// half of the two-way sync: deleting a sent mail in the ERP already moved
	// the host's copy to its trash, but deleting it in Gmail reached nothing,
	// so the ERP went on listing a message the mailbox no longer had.
	//
	// No rescue folder, unlike the junk folder, because there is only one way
	// out of a sent folder. Gmail applies the Sent label at send time and no
	// ordinary action removes it — archiving a conversation drops the Inbox
	// label, not this one — and on a host where Sent is a real folder a
	// message leaves it only by being moved or deleted. So a UID that stops
	// being returned here means somebody deleted it, which is exactly what
	// should be mirrored. The mirror is a soft delete either way, so an
	// inference that turns out wrong costs a trip to the recycle bin rather
	// than the message.
	if actual, err := s.specialFolderOf(ctx, acct, "sent"); err == nil {
		if err := s.ReconcileFlags(ctx, cfg.TenantID, acct, "SENT", actual, ""); err != nil {
			s.log.Warn("could not reconcile the sent folder", "account", acct.AccountID, "err", err)
		}
	}

	// The ping goes out only after everything is committed, and only to the
	// mailbox owner: an inbox is personal.
	if newInbox > 0 && s.live != nil {
		// 推给人，不是推给账号：收件箱是个人的，而一个人可能有好几个信箱。
		s.live.ToEmployees(ctx, cfg.TenantID, []int64{acct.EmployeeID},
			livefeed.Event{Type: livefeed.MailInbound})
	}
	return newInbox, nil
}

// syncFolder brings one folder up to date and reaches one batch further into
// its history. `logical` is our stable name (INBOX/SENT) used in storage;
// `actual` is whatever the host calls the folder.
func (s *Service) syncFolder(ctx context.Context, cfg SyncConfig, acct MailAccount, logical, actual string) (int, error) {
	state, err := s.q.GetSyncState(ctx, store.GetSyncStateParams{
		TenantID: cfg.TenantID, AccountID: acct.AccountID, Folder: logical,
	})
	if err != nil {
		state = store.GetSyncStateRow{} // never synced
	}

	// 计时分两段。fetchTook 只算「从服务器上把信搬下来」那几段，started 算
	// 整趟。分开是因为合在一起量出来的不是下载速度：解析、入库、存原件都在
	// 本机，快得多，混进去会把速度报得高出一个数量级。#389 第一版就把计时
	// 起点放在 Fetch 之后，那个 rate_kbps 量的其实是入库速度。
	started := time.Now()
	var fetchTook time.Duration

	fetchAt := time.Now()
	res, err := s.mailbox.Fetch(ctx, acct, actual, uint32(state.LastUid), cfg.BatchSize)
	fetchTook += time.Since(fetchAt)
	if err != nil {
		return 0, err
	}

	// UIDVALIDITY changing means every UID we hold now refers to something
	// else, or to nothing. Start over: the high and low marks are both
	// meaningless against the new numbering.
	if state.UidValidity != 0 && uint32(state.UidValidity) != res.UIDValidity {
		s.log.Warn("mailbox UIDVALIDITY changed, resynchronising",
			"account", acct.AccountID, "folder", logical,
			"was", state.UidValidity, "now", res.UIDValidity)
		state = store.GetSyncStateRow{}
		fetchAt = time.Now()
		res, err = s.mailbox.Fetch(ctx, acct, actual, 0, cfg.BatchSize)
		fetchTook += time.Since(fetchAt)
		if err != nil {
			return 0, err
		}
	}

	stored := 0
	// 收了多少字节、花了多久：没有这两个数，「这个信箱为什么慢」只能靠翻
	// 日志算时间差，而一封大信卡住整个信箱那次，正是因为没人看得见它。
	var bytes int64
	highest := uint32(state.LastUid)
	lowest := uint32(state.LowUid)
	ingestBatch := func(msgs []RawMessage, validity uint32) {
		for _, m := range msgs {
			bytes += int64(len(m.Raw))
			// Stamped here rather than in the adapter: the validity belongs to
			// the fetch, not to the message, and every message in one fetch
			// shares it.
			m.UIDValidity = validity
			if err := s.ingest(ctx, cfg.TenantID, acct, logical, m); err != nil {
				s.log.Warn("could not store a message",
					"account", acct.AccountID, "folder", logical, "uid", m.UID, "err", err)
				// The high-water mark still advances: a message we cannot
				// parse must not be retried on every pass for ever.
			} else {
				stored++
			}
			if m.UID > highest {
				highest = m.UID
			}
			if lowest == 0 || m.UID < lowest {
				lowest = m.UID
			}
		}
	}
	ingestBatch(res.Messages, res.UIDValidity)

	// One batch of history per pass, newest first, until the cap. low_uid==1
	// marks the bottom: the dial for "is there anything older" is not free.
	if lowest > 1 {
		held, err := s.q.CountFolder(ctx, store.CountFolderParams{
			TenantID: cfg.TenantID, AccountID: acct.AccountID, Folder: logical,
		})
		if err == nil && held < cfg.HistoryCap {
			fetchAt = time.Now()
			old, err := s.mailbox.FetchBelow(ctx, acct, actual, lowest, cfg.BatchSize)
			fetchTook += time.Since(fetchAt)
			if err != nil {
				s.log.Warn("history backfill failed",
					"account", acct.AccountID, "folder", logical, "err", err)
			} else if len(old.Messages) == 0 {
				lowest = 1 // bottom reached; stop dialling for more
			} else {
				ingestBatch(old.Messages, old.UIDValidity)
			}
		}
	}

	if err := s.q.UpsertSyncState(ctx, store.UpsertSyncStateParams{
		TenantID: cfg.TenantID, AccountID: acct.AccountID, Folder: logical,
		UidValidity: int64(res.UIDValidity), LastUid: int64(highest), LowUid: int64(lowest),
	}); err != nil {
		s.log.Error("could not record sync progress", "account", acct.AccountID, "err", err)
	}
	if stored > 0 {
		// rate_kbps 按 fetch 算，不按 took 算：它要回答的是「这个信箱的线路
		// 有多快」，而 took 里还含着本机的解析和入库。
		s.log.Info("folder synced", "account", acct.AccountID, "folder", logical,
			"new", stored, "bytes", bytes,
			"fetch", fetchTook.Round(time.Millisecond),
			"took", time.Since(started).Round(time.Millisecond),
			"rate_kbps", bytesPerSecond(bytes, fetchTook)/1024)
	}
	return stored, nil
}

// specialFolderOf resolves and remembers where this account keeps a special
// folder ("sent" or "junk"). Cached because the answer never changes and
// finding it costs a dial.
func (s *Service) specialFolderOf(ctx context.Context, acct MailAccount, kind string) (string, error) {
	key := fmt.Sprintf("%s:%d", kind, acct.AccountID)
	if v, ok := s.sentFolders.Load(key); ok {
		return v.(string), nil
	}
	var name string
	var err error
	switch kind {
	case "junk":
		name, err = s.mailbox.JunkFolder(ctx, acct)
	case "trash":
		name, err = s.mailbox.TrashFolder(ctx, acct)
	case "archive":
		// May legitimately be empty: not every host has an archive. Cached
		// either way, so a host without one is not asked again every time
		// somebody archives something.
		name, err = s.mailbox.ArchiveFolder(ctx, acct)
	default:
		name, err = s.mailbox.SentFolder(ctx, acct)
	}
	if err != nil {
		return "", err
	}
	s.sentFolders.Store(key, name)
	return name, nil
}

// ingest parses one message and files it.
// rawKeyFor is where one message's original MIME lives.
//
// Every component is load-bearing, and the two that were missing cost real
// messages. The key used to be tenant/account/uid, on the assumption that a
// UID identifies a message within a mailbox. It does not:
//
//   - A UID is unique within a *folder*, not within an account. INBOX 614,
//     SENT 614 and JUNK 614 are three different messages. With the folder left
//     out they shared one object, and whichever synced last overwrote the
//     rest. One mailbox had 88 such collisions across 176 messages - a mail
//     the person sent on the 1st was overwritten by a spam filed on the 6th,
//     and the row still pointed at it as though it were the original.
//
//   - A UID is only stable while UIDVALIDITY holds. The host is entitled to
//     renumber everything by changing it, which happens on migrations and
//     mailbox rebuilds, and the sync already handles that by starting over.
//     Without the validity in the key, the new numbering writes over the old
//     generation's objects.
//
// The damage is not only lost originals. A wrong original is worse: every
// repair pass in this service re-parses from here on the premise that the
// object is the message, so a collision turns "re-read the original and fix
// the row" into "write somebody else's mail into this row".
//
// Folder names are our own logical ones - INBOX, SENT, JUNK - not the host's,
// so they are safe in a path and stable across providers that spell their
// sent folder five different ways.
func rawKeyFor(tenantID, accountID int64, folder string, uidValidity, uid uint32) string {
	return fmt.Sprintf("mail/inbound/%d/%d/%s/%d/%d.eml",
		tenantID, accountID, folder, uidValidity, uid)
}

func (s *Service) ingest(ctx context.Context, tenantID int64, acct MailAccount, folder string, m RawMessage) error {
	parsed, err := ParseMail(m.Raw)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	sentAt := parsed.SentAt
	if sentAt.IsZero() {
		sentAt = m.InternalDate
	}
	// When the host says it arrived. Its own clock, not ours and not the
	// sender's: the Date header is written by whoever sent the mail and can
	// be wrong by accident or on purpose, while INTERNALDATE is the one
	// timestamp the mailbox itself stands behind.

	// The raw MIME goes to object storage before the row exists: a row that
	// promises a raw_key which was never written is worse than no row.
	rawKey := rawKeyFor(tenantID, acct.AccountID, folder, m.UIDValidity, m.UID)
	if s.files != nil {
		if err := s.putRaw(ctx, rawKey, m.Raw); err != nil {
			s.log.Warn("could not store raw message, keeping the parsed copy only",
				"uid", m.UID, "err", err)
			rawKey = ""
		}
	} else {
		rawKey = ""
	}

	// The host saves a copy of everything sent over SMTP into its sent folder,
	// including what the ERP itself sent. That copy used to be recognised here
	// and thrown away, on the grounds that the ERP already had a delivery
	// record for it and keeping both would list the mail twice.
	//
	// It is kept now, and the delivery record is linked to it instead. The
	// duplicate was never the real problem — the real problem was that a
	// delivery record is not a message: it has no folder and no UID, so it
	// could not be starred, archived or deleted. Keeping the copy is what
	// makes 已发送 behave like a mailbox instead of a report. The list joins
	// the record back on for what only it knows: status, and whether the
	// tracking pixel was fetched.
	var sentMessageID int64
	if folder == "SENT" {
		if key := messageKeyFromID(parsed.MessageID); key != "" {
			if m, err := s.q.FindMessageByKey(ctx, store.FindMessageByKeyParams{
				TenantID: tenantID, MessageKey: key,
			}); err == nil {
				sentMessageID = m.ID
			}
		}
	}

	threadKey, replyTo, owner := s.resolveThread(ctx, tenantID, parsed)

	id, err := s.q.InsertInbound(ctx, store.InsertInboundParams{
		SentMessageID: sentMessageID,
		TenantID:      tenantID, AccountID: acct.AccountID, OwnerID: acct.EmployeeID,
		Folder: folder, ImapUid: int64(m.UID),
		MessageID: parsed.MessageID, InReplyTo: parsed.InReplyTo,
		ReferencesIds: strings.Join(parsed.References, " "),
		ThreadKey:     threadKey, ReplyToID: replyTo,
		// 真正的回信地址、抄送，以及收信服务器验过的两个身份。
		// ReplyTo 与 ReplyToID 是两件毫不相干的事：前者是邮件头里那个地址，
		// 后者是本库里这封信答复的那一行。名字像，含义无关。
		ReplyTo: parsed.ReplyTo, Cc: parsed.CC,
		AuthSpf: parsed.AuthSPF, AuthDkim: parsed.AuthDKIM,
		FromEmail: parsed.FromEmail, FromName: parsed.FromName,
		ToEmail: parsed.ToEmail, ToAll: parsed.ToAll, Subject: parsed.Subject,
		BodyHtml: parsed.BodyHTML, BodyText: parsed.BodyText,
		// 搜索文本里放整段收件人：搜同事的名字要能搜到发给他的那封群发。
		Snippet: snippetOf(parsed), SearchText: searchTextOf(parsed.Subject, parsed.FromName, parsed.FromEmail,
			firstNonEmpty(parsed.ToAll, parsed.ToEmail), parsed.BodyText, parsed.BodyHTML),
		// Which customer this belongs to, inherited from the message it
		// answers. Zero when it answers nothing of ours, which is the
		// ordinary case.
		CustomerID: owner.CustomerID, ContactID: owner.ContactID,
		CustomerName: owner.CustomerName,
		RawKey:       rawKey, RawSize: int64(len(m.Raw)),
		IsBounce: parsed.IsBounce, HasAttachments: hasListedAttachments(parsed),
		IsRead: m.Seen,
		SentAt: pgtype.Timestamptz{Time: sentAt, Valid: !sentAt.IsZero()},
		ReceivedAt: pgtype.Timestamptz{
			Time: m.InternalDate, Valid: !m.InternalDate.IsZero(),
		},
	})
	if err != nil {
		// ON CONFLICT DO NOTHING returns no row at all, which sqlc surfaces as
		// "no rows in result set". That is the normal outcome of re-fetching a
		// UID we already hold, not a failure — reporting it as one filled the
		// log with warnings about messages that were stored correctly.
		if errors.Is(err, pgx.ErrNoRows) || strings.Contains(err.Error(), "duplicate key") {
			return nil
		}
		return err
	}

	for _, a := range parsed.Attachments {
		key := fmt.Sprintf("mail/inbound/%d/%d/att/%d-%s", tenantID, acct.AccountID, id, safeName(a.FileName))
		if s.files != nil {
			if err := s.putRaw(ctx, key, a.Data); err != nil {
				s.log.Warn("could not store an incoming attachment", "file", a.FileName, "err", err)
				key = ""
			}
		}
		if err := s.q.InsertInboundAttachment(ctx, store.InsertInboundAttachmentParams{
			TenantID: tenantID, InboundID: id, FileName: a.FileName,
			ContentType: a.ContentType, FileSize: int64(len(a.Data)), FileKey: key,
			// What the body points at when it embeds this part. Empty for an
			// ordinary attachment, which is most of them.
			ContentID: a.ContentID,
		}); err != nil {
			s.log.Warn("could not record an incoming attachment", "file", a.FileName, "err", err)
		}
	}

	// Bounce side effects only from the inbox. A bounce-shaped message the
	// host itself filed as spam must not be allowed to suppress an address.
	if parsed.IsBounce && folder == "INBOX" {
		s.applyBounce(ctx, tenantID, m.Raw, parsed)
	}
	return nil
}

// resolveThread ties a reply to the message that prompted it.
//
// Our own Message-IDs are <message_key@domain> by construction, so any id in
// In-Reply-To or References that we recognise identifies the exact send being
// answered. That is what puts a customer's reply underneath the quotation it
// concerns rather than loose in a list.
// mailOwner is which customer a received message belongs to, if any.
//
// Zero is the ordinary answer, not a failure: most of what arrives in a
// mailbox is not from a customer. Nothing here invents a customer for an
// unknown sender.
type mailOwner struct {
	CustomerID   int64
	ContactID    int64
	CustomerName string
}

func (s *Service) resolveThread(ctx context.Context, tenantID int64, p ParsedMail) (string, *int64, mailOwner) {
	candidates := append([]string{p.InReplyTo}, p.References...)
	// Our own sent copy names itself. The host hands the message back out of
	// its Sent folder carrying the Message-ID we issued, so for that one the
	// anchor is its own header rather than a reference to somebody else's.
	//
	// Checked last, so a genuine reply still wins: a mail can be both an
	// answer to us and one of ours (a colleague replying from the same
	// mailbox), and the chain it answers is the better thread.
	//
	// Without this the copy starts a thread of one — keyed on the raw
	// <uuid@domain> — while the customer's reply resolves through
	// FindMessageByKey to the ERP record's thread, keyed on the bare uuid.
	// Same conversation, two keys: 收件箱 shows the exchange and 已发送 shows
	// a lone message with no sign a reply ever came.
	candidates = append(candidates, p.MessageID)
	for i, id := range candidates {
		key := messageKeyFromID(id)
		if key == "" {
			continue
		}
		row, err := s.q.FindMessageByKey(ctx, store.FindMessageByKeyParams{
			TenantID: tenantID, MessageKey: key,
		})
		if err != nil {
			continue
		}
		msgID := row.ID
		thread := row.ThreadKey
		if thread == "" {
			thread = row.MessageKey
		}
		// Matching on its own Message-ID means this *is* the message that was
		// sent, not an answer to it, so it gets no reply_to_id — pointing a
		// message at itself would make the chain a loop.
		if i == len(candidates)-1 {
			return thread, nil, mailOwner{
				CustomerID:   row.CustomerID,
				ContactID:    row.ContactID,
				CustomerName: row.CustomerName,
			}
		}
		// The reply inherits the customer of the message it answers. This is
		// the whole first layer of linking mail to business records, and it
		// is stronger than matching the sender's address: it works when the
		// customer replies from a phone, from a colleague's account, or from
		// an address nobody has ever entered into the contact list, because
		// what identifies the conversation is the mail's own In-Reply-To
		// rather than anything about who sent it.
		return thread, &msgID, mailOwner{
			CustomerID:   row.CustomerID,
			ContactID:    row.ContactID,
			CustomerName: row.CustomerName,
		}
	}
	// Not ours. Keep the sender's own chain together: the root of References
	// is the start of their conversation, and failing that the message is the
	// start of its own.
	if len(p.References) > 0 {
		return truncate(p.References[0], 64), nil, mailOwner{}
	}
	if p.InReplyTo != "" {
		return truncate(p.InReplyTo, 64), nil, mailOwner{}
	}
	return truncate(p.MessageID, 64), nil, mailOwner{}
}

// messageKeyFromID extracts our own key out of a Message-ID we issued.
func messageKeyFromID(id string) string {
	id = strings.Trim(strings.TrimSpace(id), "<>")
	i := strings.Index(id, "@")
	if i <= 0 {
		return ""
	}
	return id[:i]
}

// applyBounce turns a delivery report into a fact about a recipient.
//
// This is the only bounce signal a mail host gives us — there is no webhook —
// so without it the needs-attention queue stays empty while mail quietly
// fails to arrive.
func (s *Service) applyBounce(ctx context.Context, tenantID int64, raw []byte, p ParsedMail) {
	for _, origID := range OriginalMessageIDs(raw) {
		key := messageKeyFromID(origID)
		if key == "" {
			continue
		}
		row, err := s.q.FindMessageByKey(ctx, store.FindMessageByKeyParams{
			TenantID: tenantID, MessageKey: key,
		})
		if err != nil {
			continue
		}
		status := "NEEDS_ATTENTION"
		reason := "对方服务器暂时退信：" + p.BounceDetail
		if p.BouncePermanent {
			status = "HARD_BOUNCED"
			reason = "地址不可达，已加入拒收名单：" + p.BounceDetail
		}
		if err := s.q.MarkTerminal(ctx, store.MarkTerminalParams{
			TenantID: tenantID, ID: row.ID, Status: status,
			LastError: p.BounceDetail, AttentionReason: reason,
		}); err != nil {
			s.log.Warn("could not record a bounce", "message", row.ID, "err", err)
			continue
		}
		// Only a permanent failure suppresses. A full mailbox or a greylist
		// is temporary, and blacklisting a customer over one would quietly
		// end the correspondence.
		if p.BouncePermanent {
			addr := p.BounceRecipient
			if addr == "" {
				addr = row.ToEmail
			}
			if addr != "" {
				// 和 worker.go 里那处同一条规矩：**拉黑失败必须喊出来**。
				// 没拉黑的死地址会被一遍遍重投，伤的是所有邮件的送达率，
				// 而默默失败的话没有任何地方看得出来。
				if err := s.Suppress(ctx, tenantID, addr, "HARD_BOUNCE", p.BounceDetail); err != nil {
					s.log.Error("硬退信地址没能拉黑，之后还会继续往这个地址发",
						"tenant", tenantID, "email", addr, "message", row.ID,
						"err", err,
						"impact", "继续投递到已确认失效的地址会拖垮发信域名声誉，影响所有邮件",
						"fix", "在「邮件 · 退信抑制」里手工把这个地址加进去")
				}
			}
		}
		s.log.Info("bounce applied", "message", row.ID, "permanent", p.BouncePermanent)
	}
}

func (s *Service) putRaw(ctx context.Context, key string, data []byte) error {
	return s.files.Put(ctx, key, bytes.NewReader(data), int64(len(data)), "message/rfc822")
}

// snippetOf is the one line of the mail the list shows beside the subject.
//
// The text part is preferred but not trusted: plenty of senders put markup in
// text/plain, and one that does would put "<!doctype html><html xmlns=..." in
// front of the person instead of the first sentence. Anything that looks like
// markup goes through the HTML path, which strips <style> and <script> bodies
// rather than just their tags — a mail's stylesheet is otherwise the first
// thing in the snippet and the longest.
func snippetOf(p ParsedMail) string {
	text := p.BodyText
	if strings.TrimSpace(text) == "" || looksLikeMarkup(text) {
		html := p.BodyHTML
		if strings.TrimSpace(html) == "" {
			html = text
		}
		text = HTMLToText(html)
	}
	text = strings.Join(strings.Fields(text), " ")
	return truncate(text, 280)
}

// searchTextOf is everything the mail says, as plain text, for the search
// index. The snippet's logic without the 280-character truncation — the same
// choice of source and the same HTML path, because a mail found by its
// opening line and lost by its third paragraph would be worse than either.
//
// The order matters. text/plain is preferred, but only if it is really plain:
// plenty of senders put a whole HTML document in it, and indexing that would
// mean a query for "content" matching class="content" and every mail with an
// embedded image matching half the base64 alphabet.
func searchTextOf(subject, fromName, fromEmail, toEmail, bodyText, bodyHTML string) string {
	text := bodyText
	if strings.TrimSpace(text) == "" || looksLikeMarkup(text) {
		html := bodyHTML
		if strings.TrimSpace(html) == "" {
			html = text
		}
		text = HTMLToText(html)
	}
	// Both paths, not just the HTML one. HTMLToText strips these on its way
	// through, so a text/plain body was the only kind that kept them — and a
	// zero-width joiner sitting inside a phrase makes that phrase unfindable
	// by anybody who types it.
	// The subject and the addresses live in here too, ahead of the body.
	//
	// Not for tidiness — for the index. A predicate spread across five columns
	// with OR cannot use the trigram index, and the planner falls back to a
	// sequential scan: measured on this mailbox, 100 ms across five columns
	// against 1.6 ms against this one. Folding the header in is what turns
	// the index from decoration into the thing that answers the query.
	//
	// The order puts the header first so a hit on a subject yields a match
	// snippet that opens with the subject, which reads as an explanation of
	// why the row matched rather than as a fragment from nowhere.
	head := strings.Join([]string{subject, fromName, fromEmail, toEmail}, " ")
	return strings.Join(strings.Fields(StripInvisible(head+" "+text)), " ")
}

// markupHead spots the openings that mean "this is a document, not a
// sentence". Deliberately anchored near the start: a plain-text mail that
// happens to mention <b> further down is still plain text.
var markupHead = regexp.MustCompile(`(?is)^\s*(<!doctype|<html|<head|<body|<table|<div|<style|<meta)`)

func looksLikeMarkup(s string) bool { return markupHead.MatchString(s) }

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func safeName(s string) string {
	repl := strings.NewReplacer("/", "_", "\\", "_", "..", "_", " ", "_")
	return truncate(repl.Replace(s), 80)
}

// NewsWaiter is the optional push side of a mailbox: an adapter that can hold
// a connection open and report "something changed" the moment it does.
// Optional by type assertion so fakes and future adapters need not fake it.
type NewsWaiter interface {
	WaitForNews(ctx context.Context, acct MailAccount, folder string, maxWait time.Duration) (bool, error)
}

// RunIdleWatchers keeps one held-open IMAP connection per active mailbox and
// syncs the moment the host reports new mail.
//
// This is the difference between "within two minutes" and "within seconds":
// the poller stays as the safety net and the historian (sent folder,
// backfill), while the watcher makes the inbox feel live. One goroutine and
// one connection per mailbox is the honest cost.
func (s *Service) RunIdleWatchers(ctx context.Context, cfg SyncConfig) {
	cfg = cfg.withDefaults()
	waiter, ok := s.mailbox.(NewsWaiter)
	if !ok {
		s.log.Info("mailbox adapter cannot push; relying on the poller alone")
		return
	}
	s.log.Info("idle watchers started")

	var mu sync.Mutex
	// 键是员工号。employees.id 是全局自增主键，跨公司不会撞——这一点是这个 map
	// 能只用员工号做键的前提，换成按公司各排各的号就得改成 (公司, 员工)。
	running := map[int64]bool{}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		for _, tenantID := range s.tenantsToServe(ctx) {
			watch := cfg
			watch.TenantID = tenantID
			// **只守有人在看的箱。**
			//
			// 一个箱一条常开连接是这套东西最贵的一项：300 人一人两箱就是
			// 600 条 IMAP 连接一直占着，而其中大部分箱当天根本没人打开过。
			//
			// IDLE 的用处是「让正在看的那个收件箱像是活的」——没人看的时候，
			// 两分钟一轮的轮询加十分钟一次的轻状态已经够了。
			accounts, err := s.q.ListActiveMailAccounts(ctx, store.ListActiveMailAccountsParams{
				TenantID: tenantID, ActiveSeconds: int32(watch.ActiveWindow.Seconds()),
			})
			if err != nil {
				// 一家公司列不出来不该让别家也停：下面的 continue 只跳过这一家。
				s.log.Warn("could not list mailboxes to watch", "tenant", tenantID, "err", err)
				continue
			}
			for _, a := range accounts {
				mu.Lock()
				already := running[a.ID]
				if !already {
					running[a.ID] = true
				}
				mu.Unlock()
				if already {
					continue
				}
				go func(cfg SyncConfig, accountID int64) {
					defer func() {
						mu.Lock()
						delete(running, accountID)
						mu.Unlock()
					}()
					s.watchMailbox(ctx, cfg, waiter, accountID)
				}(watch, a.ID)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) watchMailbox(ctx context.Context, cfg SyncConfig, waiter NewsWaiter, accountID int64) {
	backoff := time.Minute
	// 这台服务器的 IDLE 撑不撑得住。掐得太勤就停掉推送、交给轮询，见 idledrop.go。
	var health idleHealth
	for ctx.Err() == nil {
		// 没人在看了就收摊。每一圈开头查一次——IDLE 一圈最长 25 分钟，所以
		// 一个箱从「没人看」到连接真正释放最多隔一圈。管理器每分钟扫一次，
		// 人一回来它就重新开起来。
		//
		// 查不出来当作还在看：这条判断错在「多守一会儿」是浪费，错在
		// 「早收了」是收件箱不再实时，而人不会知道为什么。
		if watching, err := s.q.MailboxIsBeingRead(ctx, store.MailboxIsBeingReadParams{
			TenantID: cfg.TenantID, ID: accountID,
			ActiveSeconds: int32(cfg.ActiveWindow.Seconds()),
		}); err == nil && !watching {
			return
		}
		acct, err := s.ForAccount(ctx, cfg.TenantID, accountID)
		if err != nil {
			// Unbound or paused. The manager restarts the watch if the
			// account comes back; holding a loop open for it helps nobody.
			return
		}
		// 这台服务器的 IDLE 被判定为没用，正在冷静期：不开连接，睡到期满
		// 再试。这段时间里收信全靠两分钟一轮的轮询——新信最多晚两分钟，
		// 换掉每分钟一次的重新登录。
		if !health.idleWorthTrying(time.Now()) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(idlePauseCheckEvery):
			}
			continue
		}
		// 比 IDLE 的续命间隔略长：正常情况下是续命先到，这个只是兜底。
		startedAt := time.Now()
		news, err := waiter.WaitForNews(ctx, acct, "INBOX", IdleRestartEvery+time.Minute)
		if err != nil {
			if errors.Is(err, ErrPushUnsupported) {
				// 这台服务器没有推送这回事。别再为它挂连接——两分钟一轮的
				// 轮询本来就在跑，而且用的是连接池里的连接，比挂着一条自己
				// 的便宜。隔一阵再问一次：服务商会升级。
				s.log.Info("host has no IMAP push; leaving this mailbox to the poller",
					"account", accountID, "retry_in", idleRetryAfter)
				health.quietUntil = time.Now().Add(idleRetryAfter)
				continue
			}
			if BenignIdleDrop(err) {
				if !health.noteIdleRun(time.Now(), time.Since(startedAt), true) {
					// 连着几圈都撑不到一分半。再重连下去只是每分钟登录一次。
					s.log.Info("host keeps cutting idle connections; falling back to polling",
						"account", accountID, "quiet_for", idleRetryAfter)
					continue
				}
				// 对方挂了电话。263 几分钟就来一次，不是故障：记一条 Info 留
				// 个脚印，然后重连——不退避。退避是留给拒绝我们的服务器的。
				//
				// 但要有个最小间隔：一台连上就挂的服务器会让这个循环以握手
				// 的速度空转。几秒钟，和推送的时效性比不算什么。
				s.log.Info("idle connection closed by host, reconnecting", "account", accountID)
				backoff = time.Minute
				select {
				case <-ctx.Done():
					return
				case <-time.After(benignReconnectDelay):
				}
				continue
			}
			s.log.Warn("idle watch dropped", "account", accountID, "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			// A host that keeps refusing gets visited less and less often;
			// the poller still covers the gap.
			if backoff < 10*time.Minute {
				backoff *= 2
			}
			continue
		}
		backoff = time.Minute
		health.noteIdleRun(time.Now(), time.Since(startedAt), false)
		if news {
			if n, err := s.SyncMailbox(ctx, cfg, accountID); err != nil {
				s.log.Warn("push-triggered sync failed", "account", accountID, "err", err)
			} else if n > 0 {
				s.log.Info("mail arrived by push", "account", accountID, "new", n)
			}
		}
	}
}

// syncExtraFolders 把自建文件夹和服务器自带的真文件夹里的信也收进来。
//
// 只在全量那一档跑（有人在看的箱），跟着 syncOne 走。一个箱通常只多零到
// 三个文件夹，代价可控；真多到几十个的，每轮多花的时间也只落在那一个箱上。
//
// 收哪些文件夹**读的是登记表**，不是当场问服务器：登记发生在打开邮箱页那
// 一下（前端每次切信箱都会列一次文件夹）。两件事在现实里一起发生——页面一开，
// 列文件夹和拉列表都会打上来，而"有人在看"正是全量同步这一档的条件。这样
// 每轮同步省掉一次 LIST 往返。
//
// 每个文件夹自己一条同步游标（mail_sync_state 的主键带 folder），所以第一次
// 会把整个文件夹拉一遍，之后增量。ERP 自己挪进去的信 UID 已经在库里，
// InsertInbound 是 ON CONFLICT DO NOTHING，重复拉到只是空转。
//
// 一个文件夹失败不影响别的，也不影响收件箱——收件箱早在上面就已经交差了。
func (s *Service) syncExtraFolders(ctx context.Context, cfg SyncConfig, acct MailAccount) {
	rows, err := s.q.ListFoldersToSync(ctx, store.ListFoldersToSyncParams{
		TenantID: cfg.TenantID, AccountID: acct.AccountID,
		Roles: []string{roleCustom, roleSystem}, RowLimit: maxFoldersPerPass,
	})
	if err != nil {
		s.log.Warn("could not list folders to sync", "account", acct.AccountID, "err", err)
		return
	}
	for _, r := range rows {
		fcfg := cfg
		if r.Role == roleSystem && fcfg.HistoryCap > extraFolderHistoryCap {
			// 服务器自带、我们不认得的那些（病毒、广告、订阅）：留一层浅的
			// 就够。自建文件夹是员工自己归的类，按收件箱的深度留。
			fcfg.HistoryCap = extraFolderHistoryCap
		}
		if _, err := s.syncFolder(ctx, fcfg, acct, r.HostName, r.HostName); err != nil {
			s.log.Warn("folder sync failed", "account", acct.AccountID, "folder", r.HostName, "err", err)
		}
	}
}

// extraFolderHistoryCap 是服务器自带、ERP 不认得的那些文件夹留多少历史。
// 和垃圾邮件同一个数：旧的广告和病毒邮件是价值最低的信。
const extraFolderHistoryCap = 100

// maxFoldersPerPass 是一趟同步最多碰几个额外文件夹。
//
// 一个人能建的文件夹没有上限，每个文件夹至少一次 IMAP 往返；建了几十个的
// 账号会把自己那一格时间片吃光，挤到同一批里别人的箱。查询按「最久没同步的
// 排前面」轮着给，所以有上限也不会漏，只是最坏多等几轮。
//
// 12 是照分档的余量取的：有人在看的那一档每个箱大约 19 秒，收件箱/已发送/
// 垃圾邮件之外还剩得下十来次往返。
const maxFoldersPerPass = 12

// bytesPerSecond 是每秒多少字节，只给日志用。
func bytesPerSecond(bytes int64, took time.Duration) int64 {
	if took <= 0 {
		return 0
	}
	return int64(float64(bytes) / took.Seconds())
}

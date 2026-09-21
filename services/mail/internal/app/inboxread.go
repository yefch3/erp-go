package app

import (
	"context"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// InboundView is one received message as the API returns it.
type InboundView struct {
	ID int64
	// 这封信落在哪个信箱。搜索横跨信箱之后必须跟着信走：一封 B 箱收到的信
	// 可能是站在 A 箱里点开的，而「点回复从哪个地址发出去」得看这封信是
	// 哪个箱收到的，不能看左边高亮着谁——那就是「读 B 的信、从 A 回过去」。
	AccountID int64
	// 左栏的哪一格（INBOX / ARCHIVE / JUNK / TRASH / F:…），已发送是空串。
	// 只有单封读取填它：列表那边站在哪一格本来就知道。
	View           string
	FromEmail      string
	FromName       string
	ToEmail        string
	Subject        string
	Snippet        string
	ThreadKey      string
	IsRead         bool
	IsStarred      bool
	HasAttachments bool
	// 这封信答过没有（#364）：我们自己发出并送达的回信，或者服务器上的
	// \Answered 标志。只在列表上有值——单封读取不需要它，那一页上「回复」
	// 按钮本来就在手边。
	IsAnswered bool
	ReceivedAt time.Time
	SentAt     time.Time
	BodyHTML   string
	BodyText   string
	// The conversation quoted back inside this message, split out so the
	// reader can fold it. Empty means there was nothing worth folding, which
	// is the answer for most mail — see SplitQuotedHistory.
	QuotedHTML  string
	Attachments []Attachment
	// How many messages the list row stands for. 1 for a lone message; the
	// list collapses a conversation into one row and shows this count.
	ThreadCount int32

	// Sent folder only. "HOST" is a real message in the host's Sent folder —
	// every mailbox action works on it. "ERP" is a delivery record the host
	// never kept a copy of, shown after a grace period so a mail that was
	// genuinely sent is never missing; it has no message to act on.
	Kind     string
	ToName   string
	Status   string
	OpenedAt time.Time
	// Whether a tracking pixel actually went into this send. The third state,
	// and it cannot be inferred: "nobody opened it" and "nobody was watching"
	// are the same empty OpenedAt, and only the first says anything about the
	// recipient.
	Tracked bool

	// Which mailbox folder this copy sits in. SENT is what tells the reader to
	// show 对方是否已读; once both are an InboundView, nothing else does.
	Folder string
	// The RFC 5322 Message-ID and the size of the stored MIME — the details
	// panel, not the reader.
	MessageIDHeader string
	RawSize         int64
	// 真正的回信地址。与 FromEmail 不同时，界面要提醒——那是伪造供应商邮件
	// 骗货款最常用的一手。
	ReplyTo string
	CC      string
	// 整段 To 头，给人看的。ToEmail 只是其中第一个。存量还没补时退回 ToEmail。
	ToAll string
	// 从 ToAll 和 CC 里拆出来的人，给「回复全部」用。
	ToParties []MailParty
	CcParties []MailParty
	// 收信服务器验过的身份，只在验证通过时有值。空表示「未验证」，不是
	// 「验证失败」——老邮件在这两列存在之前就入库了。
	AuthSPF  string
	AuthDKIM string

	// Whether the original MIME is still in object storage, which is what
	// forward-as-attachment sends. The send path refuses without it anyway;
	// this is so the screen can grey the action out instead of letting
	// somebody write a whole mail and then be told it cannot go.
	HasRaw bool
}

// InboundPage is one screenful of conversations plus the counts and the
// cursor that reaches the next one.
type InboundPage struct {
	Mails  []InboundView
	Total  int64
	Unread int32
	// Empty when this is the last page. Opaque to the caller: it encodes the
	// sort position of the final row, not an offset.
	NextCursor string
	// 这一页是按会话合并的还是一行一封（见 ListMode）。
	//
	// **由服务端报出来，不由调用方传进来**：合并与否决定了这些行的含义——
	// 一行代表一条会话时，点删除要删整条；代表一封信时只删这一封。让前端
	// 自己记着当前是哪一档，就多出一个「它以为是 A、服务端按 B 给的」的
	// 缺口，而那种错的样子是删多了信。
	Mode ListMode
}

// threadRow is one conversation as the list needs it, from whichever of the
// two queries produced it. sqlc generates a row type per query and the two are
// structurally identical, so this is the seam that keeps the mapping below
// from being written twice and drifting.
type threadRow struct {
	ID                          int64
	FromEmail, FromName         string
	Subject, Snippet, ThreadKey string
	IsRead, IsStarred           bool
	HasAttachments              bool
	ThreadCount                 int32
	ReceivedAt, SentAt          pgtype.Timestamptz
	// 只有排序版查询带这两个：RawSize 是按大小排时列表要显示的数，SortKey
	// 是下一页游标的位置。其余两条查询留零值。
	RawSize int64
	SortKey string
	// 这封信答过没有（#364）。搜索那条路暂时给不出来，见 threadRowsFromSearch。
	IsAnswered bool
}

func threadRowsFromView(rows []store.ListThreadsByViewRow) []threadRow {
	out := make([]threadRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, threadRow{
			ID: r.ID, FromEmail: r.FromEmail, FromName: r.FromName,
			Subject: r.Subject, Snippet: r.Snippet, ThreadKey: r.ThreadKey,
			IsRead: r.IsRead, IsStarred: r.IsStarred, HasAttachments: r.HasAttachments,
			IsAnswered:  r.IsAnswered,
			ThreadCount: r.ThreadCount, ReceivedAt: r.ReceivedAt, SentAt: r.SentAt,
		})
	}
	return out
}

func threadRowsFromSorted(rows []store.ListThreadsByViewSortedRow) []threadRow {
	out := make([]threadRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, threadRow{
			ID: r.ID, FromEmail: r.FromEmail, FromName: r.FromName,
			Subject: r.Subject, Snippet: r.Snippet, ThreadKey: r.ThreadKey,
			IsRead: r.IsRead, IsStarred: r.IsStarred, HasAttachments: r.HasAttachments,
			IsAnswered:  r.IsAnswered,
			ThreadCount: r.ThreadCount, ReceivedAt: r.ReceivedAt, SentAt: r.SentAt,
			RawSize: r.RawSize, SortKey: r.SortKey,
		})
	}
	return out
}

// 单封档的两条。ThreadCount 给 1 而不是留零值：这一行确实代表一封信，而
// 「0 封」不是任何东西的真话。徽标的门槛是大于 1，所以两种写法在屏幕上
// 一样——区别只在哪一个日后读起来不会让人愣一下。
func threadRowsFromMessages(rows []store.ListMessagesByViewRow) []threadRow {
	out := make([]threadRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, threadRow{
			ID: r.ID, FromEmail: r.FromEmail, FromName: r.FromName,
			Subject: r.Subject, Snippet: r.Snippet, ThreadKey: r.ThreadKey,
			IsRead: r.IsRead, IsStarred: r.IsStarred, HasAttachments: r.HasAttachments,
			IsAnswered:  r.IsAnswered,
			ThreadCount: 1, ReceivedAt: r.ReceivedAt, SentAt: r.SentAt,
		})
	}
	return out
}

func threadRowsFromMessagesSorted(rows []store.ListMessagesByViewSortedRow) []threadRow {
	out := make([]threadRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, threadRow{
			ID: r.ID, FromEmail: r.FromEmail, FromName: r.FromName,
			Subject: r.Subject, Snippet: r.Snippet, ThreadKey: r.ThreadKey,
			IsRead: r.IsRead, IsStarred: r.IsStarred, HasAttachments: r.HasAttachments,
			IsAnswered:  r.IsAnswered,
			ThreadCount: 1, ReceivedAt: r.ReceivedAt, SentAt: r.SentAt,
			RawSize: r.RawSize, SortKey: r.SortKey,
		})
	}
	return out
}

func threadRowsFromSearch(rows []store.ListInboundThreadsRow) []threadRow {
	out := make([]threadRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, threadRow{
			ID: r.ID, FromEmail: r.FromEmail, FromName: r.FromName,
			Subject: r.Subject, Snippet: r.Snippet, ThreadKey: r.ThreadKey,
			IsRead: r.IsRead, IsStarred: r.IsStarred, HasAttachments: r.HasAttachments,
			ThreadCount: r.ThreadCount, ReceivedAt: r.ReceivedAt, SentAt: r.SentAt,
		})
	}
	return out
}

// ListInbound is the caller's own inbox, one page at a time.
//
// ownerID is always the caller. There is deliberately no way to pass another
// person's id here: the supervisor view reads through its own scoped surface,
// and this one answers only "my mail".
//
// Paging is keyset: cursor names the last row of the previous page and the
// next one starts strictly after it. An empty cursor is the first page. Mail
// arriving while somebody reads therefore cannot shift the boundary and make
// a conversation show up twice or slip past unseen, which is exactly what
// OFFSET does on a list that grows at the top.
// accountID 是「只看这个信箱」，0 表示我全部信箱。产品口径是按邮箱分、
// 左侧切换，所以正常情况下它总是有值；0 那一档留给还没带这个参数的旧前端
// （部署顺序是后端先发前端后发，那几分钟里列表不能空）。
//
// sort 是按哪一列排（见 ListSort）。默认的日期倒序走靠索引的那条查询；其余
// 任何一档走 ListThreadsByViewSorted。**带关键词时不能排序**：关键词走的是
// 按会话匹配的搜索查询，那条按时间给结果——同一个词按两种口径给两份结果，
// 比不支持更糟，所以直接报错而不是悄悄按日期排。
// unreadOnly 是列表上那个「只看未读」开关。按**会话**筛（any_unread），和列表
// 的行是一回事：一行代表一条会话，里面还有没读的信就该留下。
//
// **带关键词时它不生效**，和排序一样：搜索走的是另一条按会话匹配的查询，
// 在那条路上再叠一个筛选是第三种口径。前端搜索时也不显示这个开关。
func (s *Service) ListInbound(ctx context.Context, tenantID, ownerID, accountID int64, keyword, view, cursor string, size int32, sort ListSort, unreadOnly bool) (InboundPage, error) {
	_, size = normalizePage(1, size)
	// An unknown view falls back to the inbox proper rather than erroring:
	// the worst a bad parameter can do is show the default slice.
	view = normalizeView(view)
	sort, err := normalizeListSort(sort, inboundSortColumns)
	if err != nil {
		return InboundPage{}, err
	}
	// 只有空格的关键词不算关键词。前端的排序栏用的也是 trim 过的判断；
	// 两边不一致的样子是排序栏显示着、请求却被这里拒掉。
	keyword = strings.TrimSpace(keyword)
	if keyword != "" && !sort.isDefault() {
		return InboundPage{}, errSortNotWithKeyword()
	}
	// 「不筛选」在 SQL 里是 NULL，不是 0：0 是一个真实存在的 account_id
	// 取值范围之外的数没错，但用它当哨兵就得在四条查询里各写一次判断，
	// 而漏写的那一条不会报错、只会安静地一条都不返回。
	var acct *int64
	if accountID > 0 {
		acct = &accountID
		// 有人在看这个箱。写在列表这一条路上，因为它是「打开邮箱」的必经
		// 之处——开一封信、翻一页都会先经过它。
		//
		// 下一批按这个值分档：有人看的箱全量同步，没人看的只刷未读数。
		// 现在就开始写，是因为分档那一刻需要的是**历史**——列是空的话，
		// 改完之后第一个小时里所有箱都算「没人看」，于是谁都收不到信。
		s.touchMailboxRead(ctx, tenantID, accountID)
	}

	// One row per conversation, not per message: the newest message speaks
	// for the thread and carries a count. Opening it shows the whole
	// exchange, which the reading page has done since the conversation view.
	//
	// Two paths, and the split is about what can be precomputed. Without a
	// keyword the answer is a fact about the mailbox, and mail_thread_view
	// holds it already - the page reads the twenty-five rows it shows. With
	// one, the set of conversations depends on the search term, so it has to
	// be worked out per request, which is what the older query does. Search
	// is rare and its own screen; the plain list is loaded all day.
	// 合并还是一封一行，由这个人自己的设置说了算（见 ListMode）。读的是
	// 一张按主键取一行的小表，和下面那条列表查询比可以忽略不计。
	mode := s.ListMode(ctx, tenantID, ownerID)

	var rows []threadRow
	var total int64
	switch {
	// ---- 单封档。两条就够，比合并档少一条：合并档要为「带关键词」另开一
	// 条查询，是因为 mail_thread_view 预计算不出「含这个词的会话」；一行
	// 一封没有这个问题，条件直接加在行上。
	case !mode.merged() && sort.isDefault():
		at, id, err := decodeCursor(cursor)
		if err != nil {
			return InboundPage{}, err
		}
		flat, err := s.q.ListMessagesByView(ctx, store.ListMessagesByViewParams{
			TenantID: tenantID, OwnerID: ownerID, AccountID: acct, View: view,
			Keyword: keyword,
			// 带关键词时「只看未读」不生效，和合并档一样（那边带关键词走的
			// 是另一条根本没有这个参数的查询）。同一个开关在两档里含义不同
			// 的话，人换一次档就得重新学一遍。
			UnreadOnly: unreadOnly && keyword == "",
			CursorAt:   at, CursorID: id, RowLimit: size,
		})
		if err != nil {
			return InboundPage{}, err
		}
		rows = threadRowsFromMessages(flat)
		if total, err = s.q.CountMessagesByView(ctx, store.CountMessagesByViewParams{
			TenantID: tenantID, OwnerID: ownerID, AccountID: acct, View: view,
			Keyword: keyword, UnreadOnly: unreadOnly && keyword == "",
		}); err != nil {
			return InboundPage{}, err
		}
	case !mode.merged():
		// 排序版。走到这里 keyword 必然是空的——上面那道
		// errSortNotWithKeyword 已经把「带词又排序」挡掉了。
		key, id, err := decodeSortCursor(cursor, sort)
		if err != nil {
			return InboundPage{}, err
		}
		sorted, err := s.q.ListMessagesByViewSorted(ctx, store.ListMessagesByViewSortedParams{
			TenantID: tenantID, OwnerID: ownerID, AccountID: acct, View: view,
			SortBy: sort.By, SortDir: sort.Dir,
			StarFirst: sort.StarFirst, UnreadFirst: sort.UnreadFirst,
			CursorKey: key, CursorID: id, RowLimit: size, UnreadOnly: unreadOnly,
		})
		if err != nil {
			return InboundPage{}, err
		}
		rows = threadRowsFromMessagesSorted(sorted)
		if total, err = s.q.CountMessagesByView(ctx, store.CountMessagesByViewParams{
			TenantID: tenantID, OwnerID: ownerID, AccountID: acct, View: view,
			UnreadOnly: unreadOnly,
		}); err != nil {
			return InboundPage{}, err
		}
	// ---- 合并档，一直以来的三条。
	case keyword == "" && sort.isDefault():
		at, id, err := decodeCursor(cursor)
		if err != nil {
			return InboundPage{}, err
		}
		fast, err := s.q.ListThreadsByView(ctx, store.ListThreadsByViewParams{
			TenantID: tenantID, OwnerID: ownerID, AccountID: acct, View: view,
			CursorAt: at, CursorID: id, RowLimit: size, UnreadOnly: unreadOnly,
		})
		if err != nil {
			return InboundPage{}, err
		}
		rows = threadRowsFromView(fast)
		if total, err = s.q.CountThreadsByView(ctx, store.CountThreadsByViewParams{
			TenantID: tenantID, OwnerID: ownerID, AccountID: acct, View: view,
			UnreadOnly: unreadOnly,
		}); err != nil {
			return InboundPage{}, err
		}
	case keyword == "":
		// 排序版。游标的形状和上面那条不同（带着列、方向和排序键），
		// 所以解法也不同；前端换排序时清游标，两种形状不会串。
		key, id, err := decodeSortCursor(cursor, sort)
		if err != nil {
			return InboundPage{}, err
		}
		sorted, err := s.q.ListThreadsByViewSorted(ctx, store.ListThreadsByViewSortedParams{
			TenantID: tenantID, OwnerID: ownerID, AccountID: acct, View: view,
			SortBy: sort.By, SortDir: sort.Dir,
			StarFirst: sort.StarFirst, UnreadFirst: sort.UnreadFirst,
			CursorKey: key, CursorID: id, RowLimit: size, UnreadOnly: unreadOnly,
		})
		if err != nil {
			return InboundPage{}, err
		}
		rows = threadRowsFromSorted(sorted)
		// 总数和排序无关，和不排序那条用同一个数。
		if total, err = s.q.CountThreadsByView(ctx, store.CountThreadsByViewParams{
			TenantID: tenantID, OwnerID: ownerID, AccountID: acct, View: view,
			UnreadOnly: unreadOnly,
		}); err != nil {
			return InboundPage{}, err
		}
	default:
		at, id, err := decodeCursor(cursor)
		if err != nil {
			return InboundPage{}, err
		}
		slow, err := s.q.ListInboundThreads(ctx, store.ListInboundThreadsParams{
			TenantID: tenantID, OwnerID: ownerID, AccountID: acct,
			Keyword: keyword, View: view,
			CursorAt: at, CursorID: id, RowLimit: size,
		})
		if err != nil {
			return InboundPage{}, err
		}
		rows = threadRowsFromSearch(slow)
		// Still counted: the person wants to know how much mail is in here,
		// and keyset paging only replaces how pages are reached, not what
		// they show.
		if total, err = s.q.CountInboundThreads(ctx, store.CountInboundThreadsParams{
			TenantID: tenantID, OwnerID: ownerID, AccountID: acct,
			Keyword: keyword, View: view,
		}); err != nil {
			return InboundPage{}, err
		}
	}
	// 徽标和列表是同一个口径：切到哪个箱，数的就是哪个箱。不然切过去看着
	// 五封信而徽标写 12，那个数字指的是两个箱加起来。
	unread, err := s.q.CountUnread(ctx, store.CountUnreadParams{
		TenantID: tenantID, OwnerID: ownerID, AccountID: acct,
	})
	if err != nil {
		unread = 0
	}

	out := make([]InboundView, 0, len(rows))
	for _, r := range rows {
		v := InboundView{
			ID: r.ID, FromEmail: r.FromEmail, FromName: r.FromName,
			Subject: r.Subject, Snippet: r.Snippet, ThreadKey: r.ThreadKey,
			IsRead: r.IsRead, IsStarred: r.IsStarred, HasAttachments: r.HasAttachments,
			IsAnswered:  r.IsAnswered,
			ThreadCount: r.ThreadCount, RawSize: r.RawSize,
		}
		if r.ReceivedAt.Valid {
			v.ReceivedAt = r.ReceivedAt.Time
		}
		if r.SentAt.Valid {
			v.SentAt = r.SentAt.Time
		}
		out = append(out, v)
	}

	page := InboundPage{Mails: out, Total: total, Unread: int32(unread), Mode: mode}
	// A short page is the end of the list. A full one might be, and offering
	// a next page that turns out empty is a smaller sin than hiding mail.
	if int32(len(out)) == size && size > 0 {
		last := out[len(out)-1]
		if keyword == "" && !sort.isDefault() {
			page.NextCursor = encodeSortCursor(sort, rows[len(rows)-1].SortKey, last.ID)
		} else {
			page.NextCursor = encodeCursor(sortTime(last), last.ID)
		}
	}
	return page, nil
}

// sortTime mirrors the query's ORDER BY, which is received_at — when the mail
// host says it arrived. The cursor has to name the same instant the ordering
// used, or a page boundary lands in the wrong place, so these two move
// together or not at all.
func sortTime(v InboundView) time.Time {
	return v.ReceivedAt
}

// The cursor is a position, not a page number: the sort key of the last row
// shown. Encoded so it reads as an opaque token — nothing downstream should
// be tempted to do arithmetic on it.
func encodeCursor(at time.Time, id int64) string {
	return base64.RawURLEncoding.EncodeToString(
		[]byte(strconv.FormatInt(at.UTC().UnixMicro(), 10) + ":" + strconv.FormatInt(id, 10)))
}

func decodeCursor(cursor string) (pgtype.Timestamptz, int64, error) {
	if cursor == "" {
		return pgtype.Timestamptz{}, 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return pgtype.Timestamptz{}, 0, errBadCursor()
	}
	micros, idPart, ok := strings.Cut(string(raw), ":")
	if !ok {
		return pgtype.Timestamptz{}, 0, errBadCursor()
	}
	us, err := strconv.ParseInt(micros, 10, 64)
	if err != nil {
		return pgtype.Timestamptz{}, 0, errBadCursor()
	}
	id, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil {
		return pgtype.Timestamptz{}, 0, errBadCursor()
	}
	return pgtype.Timestamptz{Time: time.UnixMicro(us).UTC(), Valid: true}, id, nil
}

func errBadCursor() error {
	return apierr.Invalid("NT_BAD_CURSOR", "翻页位置无效，请回到第一页")
}

// GetInbound opens one message, marking it read as a side effect.
//
// Marking on open rather than by a separate call because that is what opening
// means: the person has seen it. This is our own inbox, so unlike the
// customer-side "opened" signal there is nothing probabilistic about it.
func (s *Service) GetInbound(ctx context.Context, tenantID, ownerID, id int64) (InboundView, error) {
	return s.inboundFor(ctx, tenantID, ownerID, id, true)
}

// inboundFor 是 GetInbound 的本体，多一个「要不要标已读」。
//
// **监管那条路读信不标已读**（supervision.go）。老板看一眼别人的收件箱，
// 不该把那封信在员工自己的列表里变成已读——更不该把 Seen 推到邮件服务器上，
// 那是员工在 Foxmail、在手机上也会看到的一次改动。而"老板看过了"这件事，
// 该记在监管日志里，不是记在员工的信上。
func (s *Service) inboundFor(ctx context.Context, tenantID, ownerID, id int64, markRead bool) (InboundView, error) {
	row, err := s.q.GetInbound(ctx, store.GetInboundParams{TenantID: tenantID, ID: id})
	if err != nil {
		return InboundView{}, errNotFound()
	}
	// Owner only. Not-found rather than forbidden: whether a given mail
	// exists in somebody else's inbox is itself not the caller's to learn.
	if row.OwnerID != ownerID {
		return InboundView{}, errNotFound()
	}

	if markRead && !row.IsRead {
		touched, err := s.q.MarkInboundRead(ctx, store.MarkInboundReadParams{
			TenantID: tenantID, OwnerID: ownerID, ID: id,
		})
		if err != nil {
			s.log.Warn("could not mark a mail read", "id", id, "err", err)
		}
		// Opening a mail here marks it read in the real mailbox too, which is
		// what anybody who also uses Gmail expects: they read it once.
		for _, t := range touched {
			s.queueFlagWrite(ctx, tenantID, t.AccountID, ownerID, t.Folder, t.ImapUid, flagSeen, true)
		}
	}

	v := InboundView{
		ID: row.ID, AccountID: row.AccountID,
		FromEmail: row.FromEmail, FromName: row.FromName,
		ToEmail: row.ToEmail, Subject: row.Subject, ThreadKey: row.ThreadKey,
		// 标过就是已读；没标（监管在看）就照实说。
		IsRead: markRead || row.IsRead, HasAttachments: row.HasAttachments,
		HasRaw: row.RawKey != "",
		Folder: row.Folder, View: row.View, MessageIDHeader: row.MessageID, RawSize: row.RawSize,
		ReplyTo: row.ReplyTo, CC: row.Cc,
		AuthSPF: row.AuthSpf, AuthDKIM: row.AuthDkim,
		// 存量还没补回来的行 to_all 是空的：退回第一个收件人，至少和从前一样，
		// 而不是显示一个空的「收件人」。
		ToAll:     firstNonEmpty(row.ToAll, row.ToEmail),
		ToParties: parseParties(firstNonEmpty(row.ToAll, row.ToEmail)),
		CcParties: parseParties(row.Cc),
		// Null for anything the inbox reads: an inbound mail has no delivery
		// record, and coalesce already turned "no row" into the empty answer.
		Status: row.SentStatus, Tracked: row.SentTracked,
		// Sanitised on the way out, not just on the way in: this HTML came
		// from the wild, and it is about to be rendered inside our page.
		// Read with the wider reader policy: this goes into a sandboxed
		// frame, where a sender's stylesheet cannot reach our page.
		//
		// Then the pictures are swapped for our own copies, so that opening
		// this mail does not tell the sender it was opened. After sanitising,
		// deliberately: the substitution matches the addresses as the reader
		// will see them, and a signed storage URL never has to survive the
		// sanitiser's own URL policy.
		BodyText: row.BodyText,
	}
	// Sanitise, then localise the pictures, then fold — in that order. The
	// fold parses the markup, so it has to run on the form the reader will
	// actually be given; folding first and sanitising afterwards would mean
	// the sanitiser could move the boundary out from under the split.
	//
	// The sanitised body is kept as its own value because the attachment list
	// below has to be filtered against it. After the swap there is no cid:
	// left in the markup to test — it has all become signed storage URLs —
	// so asking the finished body which parts it embedded would always answer
	// "none", and every signature logo would go back to being listed.
	// Two kinds of picture, and they have to be swapped on opposite sides of
	// the sanitiser.
	//
	// Embedded ones go first, before sanitising. The reader policy allows http
	// and https and nothing else, so it does not merely reject a cid: src — it
	// strips the attribute, leaving an <img> with no source at all. That is
	// what the broken glyph in a signature really was: not a src the browser
	// could not follow, but no src whatsoever. Swapping first means the
	// sanitiser sees an ordinary https storage URL and vets it like any other.
	//
	// Remote ones go after, because their keys were recorded in the form the
	// sanitiser produces — it percent-encodes spaces in URLs on the way
	// through, and matching the raw form would miss those.
	embedded := s.embeddedSwap(ctx, tenantID, id)
	// 补链接放在净化之后：只加锚点，不做任何净化，输入必须是已经过滤干净的。
	// 发信方把地址写成光秃秃的文字是常事（事务性邮件尤其多），净化器不管这个
	// ——它只负责把危险的东西去掉，不负责把不是链接的变成链接。
	sanitised := LinkifyBareURLs(SanitizeForReading(s.localiseImages(ctx, row.BodyHtml, embedded)))
	// 自家像素在这里拆掉，拆在本地化之后：图片缓存刻意不缓存我们自己的主机，
	// 于是那条地址会原样留到浏览器手里，由浏览器去把它拉一次 —— 那正是它要
	// 记录的「打开」。见 ownpixel.go。
	v.BodyHTML, v.QuotedHTML = SplitQuotedHistory(
		stripOwnPixel(
			s.localiseImages(ctx, sanitised, s.swapForMessage(ctx, tenantID, id)),
			s.selfHost))
	if row.ReceivedAt.Valid {
		v.ReceivedAt = row.ReceivedAt.Time
	}
	if row.SentAt.Valid {
		v.SentAt = row.SentAt.Time
	}
	if row.SentOpenedAt.Valid {
		v.OpenedAt = row.SentOpenedAt.Time
	}

	atts, err := s.q.ListInboundAttachments(ctx, store.ListInboundAttachmentsParams{
		TenantID: tenantID, InboundID: id,
	})
	if err == nil {
		for _, a := range atts {
			key, size := latestFile(a.FileKey, a.FileSize, a.RevVersion, a.RevFileKey, a.RevFileSize)
			v.Attachments = append(v.Attachments, Attachment{
				ID: a.ID, FileName: a.FileName, ContentType: a.ContentType,
				FileSize: size, FileKey: key, ContentID: a.ContentID,
				Revision: a.RevVersion,
			})
		}
		// Parts the body has already shown inline are not attachments to a
		// reader, whatever they are to the MIME structure. Tested against the
		// stored body, which is where the cid: references still live, and
		// against the swap, so a part we failed to resolve stays in the list
		// rather than vanishing from both the body and the attachments.
		v.Attachments = hideEmbedded(v.Attachments, row.BodyHtml, embedded)
		// Signed here rather than at ingest: a URL minted when the mail
		// arrived would have expired long before anybody opened it.
		v.Attachments = s.signDownloads(ctx, v.Attachments)
	}
	return v, nil
}

// ThreadItem is one turn of a conversation, either direction.
type ThreadItem struct {
	Direction  string
	ID         int64
	Subject    string
	Body       string
	Quoted     string
	BodyFormat string
	// Counterparty 在两个方向上说的**不是同一件事**：我发出的那行它是收件人，
	// 收到的那行它是发件人。界面上却是同一列，于是一条会话里上下两行的地址
	// 一个是「发给谁」一个是「谁发的」，看的人无从分辨。留着不动是因为别处
	// 在用；下面四个才是两腿含义一致的。
	Counterparty string
	Who          string
	At           time.Time
	FromEmail    string
	FromName     string
	// 整段收件人，不是第一个：群发给七个人的信这里要看到七个。
	ToAll string
	// 只有收到的那一腿有：email_messages 上没有存抄送。
	Cc string
	// 这一封自己带的附件。内嵌图片不在其中——那是正文的一部分，已经渲染过了。
	Attachments []Attachment
	// 只有「我发出」那一腿有，而且只在本地「已发送」里留着那一份时才有：
	// 那一份在 email_inbound 里的编号。上面 Attachments 的编号属于它。
	//
	// 预览、在线编辑、转 Excel 三条路都要「这个附件属于哪封信」，而它们只认
	// 收件那张表。0 = 没对上，那三条路对这一条走不通（下载照常）。
	LocalMailID int64
}

// accountOfMessage 问「这封信在哪个信箱」，答不上来就返回 nil = 不限定。
//
// owner 限定在查询里（GetInbound 只按 tenant+id 取，所以这里要自己比一次
// owner_id）：会话标识是猜得出来的，凭一个猜来的 id 打开别人的会话不行。
func (s *Service) accountOfMessage(ctx context.Context, tenantID, ownerID, messageID int64) *int64 {
	if messageID <= 0 {
		return nil
	}
	row, err := s.q.GetInbound(ctx, store.GetInboundParams{TenantID: tenantID, ID: messageID})
	if err != nil || row.OwnerID != ownerID {
		return nil
	}
	id := row.AccountID
	return &id
}

// GetMailThread returns one conversation, oldest first, both directions.
//
// Inbound HTML is sanitised on the way out, same as GetInbound: these bodies
// came from the wild and are about to be rendered inside our page. Outbound
// bodies were sanitised when they were composed.
// fromMessageID 是「从哪一封信点进来的」，用来定位读哪个信箱那一份；
// 0 表示不限定（旧前端），读这个人名下所有信箱。
func (s *Service) GetMailThread(ctx context.Context, tenantID, ownerID, fromMessageID int64, threadKey string) ([]ThreadItem, error) {
	if strings.TrimSpace(threadKey) == "" {
		return nil, apierr.Invalid("NT_THREAD_KEY_REQUIRED", "缺少会话标识")
	}
	// 那封信属于哪个信箱，就只读那个信箱这一份。查不到（别人的信、已删）
	// 就退回不限定——比报错好：这是读，读多了看得见，读不到看不见。
	accountID := s.accountOfMessage(ctx, tenantID, ownerID, fromMessageID)
	rows, err := s.q.ListThread(ctx, store.ListThreadParams{
		TenantID: tenantID, OwnerID: ownerID, AccountID: accountID, ThreadKey: threadKey,
	})
	if err != nil {
		return nil, err
	}
	// One query for the whole conversation's cached pictures, and one
	// signature per distinct object. A sixteen-turn thread quoting the same
	// signature logo throughout would otherwise be sixteen round trips.
	swaps := s.swapForThread(ctx, tenantID, ownerID, threadKey)
	embedded := s.embeddedSwapForThread(ctx, tenantID, ownerID, threadKey)

	// One query for the conversation's attachments, keyed by direction and id
	// because the two legs number their rows in different tables and an
	// inbound 7 is not an outbound 7.
	files := map[string][]Attachment{}
	// 「我发出」那几条在本地留底的那一份带的附件，按留底那封信的编号分。
	//
	// 单独取而不是从上面那张表里挑：那张表按信箱限定，而留底可能在这个人的
	// **另一个箱**里（见 ListSentCopyAttachments）。
	copyFiles := map[int64][]Attachment{}
	// 我们自己发出去的那几条，正文里可能带着一条**当时**签发的存储地址：写信框
	// 把阅读视图那段 HTML 抄进了引用（见 quotedimages.go）。那条地址早过期了，
	// 回头看已发送就是一个裂开的图标。这张表按对象 key 给出刚签的一条。
	freshImages := map[string]string{}

	fs, err := s.q.ListThreadAttachments(ctx, store.ListThreadAttachmentsParams{
		TenantID: tenantID, OwnerID: ownerID, AccountID: accountID, ThreadKey: threadKey,
	})
	if err != nil {
		// The bodies are worth showing without the file list; a thread that
		// refuses to open because one join failed is the worse outcome.
		s.log.Warn("could not load thread attachments", "thread", threadKey, "err", err)
		fs = nil
	}
	cs, err := s.q.ListSentCopyAttachments(ctx, store.ListSentCopyAttachmentsParams{
		TenantID: tenantID, OwnerID: ownerID, ThreadKey: threadKey,
	})
	if err != nil {
		// 退回今天的样子：那几条只能下载。同上，不值得让整条会话打不开。
		s.log.Warn("could not load the sent copies' attachments", "thread", threadKey, "err", err)
		cs = nil
	}

	// 两批一起签。Signed once for the whole conversation, and here rather than
	// at ingest: a URL minted when the mail arrived would have expired long
	// before anybody opened the thread.
	flat := make([]Attachment, 0, len(fs)+len(cs))
	for _, f := range fs {
		key, size := latestFile(f.FileKey, f.FileSize, f.RevVersion, f.RevFileKey, f.RevFileSize)
		flat = append(flat, Attachment{
			ID: f.ID, FileName: f.FileName, ContentType: f.ContentType,
			FileSize: size, FileKey: key, ContentID: f.ContentID,
			Revision: f.RevVersion,
		})
	}
	for _, c := range cs {
		key, size := latestFile(c.FileKey, c.FileSize, c.RevVersion, c.RevFileKey, c.RevFileSize)
		flat = append(flat, Attachment{
			ID: c.ID, FileName: c.FileName, ContentType: c.ContentType,
			FileSize: size, FileKey: key, ContentID: c.ContentID,
			Revision: c.RevVersion,
		})
	}
	flat = s.signDownloads(ctx, flat)
	// PreviewURL 而不是 DownloadURL：前者是「浏览器就地渲染」那一条，后者
	// 带下载附件的处置头，塞进 <img> 只会让浏览器去下载一个文件。
	for _, a := range flat {
		if a.FileKey != "" && a.PreviewURL != "" {
			freshImages[a.FileKey] = a.PreviewURL
		}
	}
	for i, f := range fs {
		k := f.Direction + ":" + strconv.FormatInt(f.MessageID, 10)
		files[k] = append(files[k], flat[i])
	}
	for j, c := range cs {
		copyFiles[c.InboundID] = append(copyFiles[c.InboundID], flat[len(fs)+j])
	}

	// 我们自己发出去的那几条，在本地「已发送」里留着的那一份是哪一行。
	// 见 ListThreadSentCopies：附件的预览、在线编辑、转 Excel 都按那一份走，
	// 因为发件附件和收件附件是两套编号，而那三条路只认收件那一套。
	type sentCopy struct {
		id   int64
		body string
	}
	copies := map[int64]sentCopy{}
	if cs, err := s.q.ListThreadSentCopies(ctx, store.ListThreadSentCopiesParams{
		TenantID: tenantID, OwnerID: ownerID, ThreadKey: threadKey,
	}); err == nil {
		for _, c := range cs {
			copies[c.SentMessageID] = sentCopy{id: c.ID, body: c.BodyHtml}
		}
	} else {
		// 对不上就退回今天的样子：能下载，不能预览、不能转。比整条会话打不开好。
		s.log.Warn("could not load the thread's sent copies", "thread", threadKey, "err", err)
	}

	out := make([]ThreadItem, 0, len(rows))
	for _, r := range rows {
		body, quoted := r.Body, ""
		if r.BodyFormat == "HTML" {
			// The fold matters most here and for the reason this view exists:
			// turn sixteen of a conversation is turns one to fifteen stacked
			// up, and the thread already shows those separately.
			//
			// **两个方向都折。** 原来只折收到的，理由大概是"我们自己写的还要
			// 折什么"——可回复带的引用恰恰是最长的那一段：写的两行在最上面，
			// 底下是整条往来。不折的话，会话里我们发出的每一条都把历史再摊
			// 一遍，正是这个视图要消灭的东西。
			if r.Direction == "IN" {
				// Embedded before the sanitiser, remote after — see GetInbound.
				body = LinkifyBareURLs(stripOwnPixel(
					s.localiseImages(ctx,
						SanitizeForReading(s.localiseImages(ctx, body, embedded[r.ID])),
						swaps[r.ID]),
					s.selfHost))
			} else {
				// 我们自己发出去的：引用里借来的那张图，地址是发信当天签的，
				// 现在早过期了。按 key 换成刚签的一条。
				body = refreshStorageImageLinks(body, freshImages)
			}
			body, quoted = SplitQuotedHistory(body)
		}
		v := ThreadItem{
			Direction: r.Direction, ID: r.ID, Subject: r.Subject,
			Body: body, Quoted: quoted, BodyFormat: r.BodyFormat,
			Counterparty: r.Counterparty, Who: r.Who,
			FromEmail: r.FromEmail, FromName: r.FromName,
			ToAll: r.ToAll, Cc: r.Cc,
			// 内嵌图片在这里剔除，而不是在 SQL 里：判断的依据是「正文有没有
			// 真的引用那个 cid」，而正文只有到这一步才拿得到。同 GetInbound。
			Attachments: hideEmbedded(
				files[r.Direction+":"+strconv.FormatInt(r.ID, 10)],
				r.Body, embedded[r.ID]),
		}
		// 会话里「我们发出去的」那一条：附件改用本地「已发送」里那一份的，
		// 编号也一起给出去（LocalMailID）。
		//
		// 这三条路——在线 Office、浏览器画表格、转 Excel——按定义只在
		// email_inbound_attachments 里找，而发件附件不在那张表里。给了本地那
		// 一份，三条路就都通了：**同一个文件，不再是上面不能转、下面能转**。
		//
		// 从前这里是把 office 标记摘掉，让按钮干脆别出现（2026-09-14）。表格
		// 那一档摘不掉——它由浏览器按扩展名自己判断，服务端说了不算——于是
		// 发出去的 .xlsx 上预览按钮还在，点了却去收件那边找一个发件编号，
		// 报「这个表格打不开，可能是文件损坏」，而文件好好的（2026-09-15 实测）。
		//
		// 对不上本地那一份时（信箱不留已发送、或者还没同步回来）仍然按老办法
		// 摘掉 office 标记，并且不给 LocalMailID——前端据此不显示预览按钮。
		// 下载两种情况都不受影响。
		//
		// "direct"（图片、PDF）从来不用管：那一档的地址在上一步就签好了。
		if r.Direction != "IN" {
			copy, ok := copies[r.ID]
			// 附件也要有：对上了一份空的（同步回来的那封没存下附件）就不算数，
			// 换过去只会让这一条的文件列表凭空消失。
			local := hideEmbedded(copyFiles[copy.id], copy.body, embedded[copy.id])
			if ok && len(local) > 0 {
				v.LocalMailID = copy.id
				v.Attachments = local
			} else {
				for i := range v.Attachments {
					if v.Attachments[i].PreviewKind == PreviewOffice {
						v.Attachments[i].PreviewKind = ""
					}
				}
			}
		}
		if r.At.Valid {
			v.At = r.At.Time
		}
		out = append(out, v)
	}
	return out, nil
}

// MarkInbound is inbox housekeeping: read/unread, star, archive, trash.
//
// Applied here first and queued for the host second: what the person sees has
// to change the moment they click, and the mailbox catches up within seconds.
// A nil flag leaves that flag alone. Owner-scoped in the query: marking a mail
// that is not the caller's is a silent no-op, not information.
//
// wholeThread applies the change to every message of the conversation. The
// list shows one row per conversation, so archiving from there has to move
// the conversation; moving only its newest message would leave the row in
// place, one message lighter.
func (s *Service) MarkInbound(ctx context.Context, tenantID, ownerID, id int64, read, starred, archived, deleted, notJunk *bool, wholeThread bool) error {
	if wholeThread {
		row, err := s.q.GetInbound(ctx, store.GetInboundParams{TenantID: tenantID, ID: id})
		// A loose message has no thread key to spread across, and somebody
		// else's mail is not ours to look up — both fall through to marking
		// the single row, which is owner-scoped in its own right.
		if err == nil && row.OwnerID == ownerID && row.ThreadKey != "" {
			// AccountID 跟着这封信走，不是可省的。同一条会话可能同时在
			// 263 和 Gmail 两个信箱里（客户抄送了两个地址），不带信箱的话，
			// 在这边点归档会把那边那份也归档、点删除会把那边那份也删掉，
			// 而且**不会报错**——sqlc 的 params 是个结构体，少填一个字段
			// 只是默认零值，编译器一个字都不说。
			touched, err := s.q.SetThreadFlags(ctx, store.SetThreadFlagsParams{
				TenantID: tenantID, OwnerID: ownerID, AccountID: row.AccountID,
				ThreadKey: row.ThreadKey,
				Read:      read, Starred: starred, Archived: archived, Deleted: deleted,
				NotJunk: notJunk,
			})
			if err != nil {
				return err
			}
			rows := touchedThread(touched)
			publishFlags(ctx, s, tenantID, ownerID, rows, read != nil, starred != nil)
			publishMoves(ctx, s, tenantID, ownerID, rows, archived != nil, deleted != nil, notJunk != nil)
			return nil
		}
	}
	touched, err := s.q.SetInboundFlags(ctx, store.SetInboundFlagsParams{
		TenantID: tenantID, OwnerID: ownerID, ID: id,
		Read: read, Starred: starred, Archived: archived, Deleted: deleted,
		NotJunk: notJunk,
	})
	if err != nil {
		return err
	}
	// All four go up to the host now. Read and star are flags; archive and
	// trash are moves between folders, and a host without an archive folder
	// simply keeps its mail where it is — see publishMove.
	rows := touchedSingle(touched)
	publishFlags(ctx, s, tenantID, ownerID, rows, read != nil, starred != nil)
	publishMoves(ctx, s, tenantID, ownerID, rows, archived != nil, deleted != nil, notJunk != nil)
	return nil
}

// touchedRow is what a housekeeping update reports back about one message.
type touchedRow struct {
	accountID int64
	folder    string
	uid       int64
	messageID string
	read      bool
	starred   bool
	archived  bool
	deleted   bool
	notJunk   bool
}

func touchedSingle(rows []store.SetInboundFlagsRow) []touchedRow {
	out := make([]touchedRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, touchedRow{
			accountID: r.AccountID, folder: r.Folder, uid: r.ImapUid,
			messageID: r.MessageID, read: r.IsRead, starred: r.IsStarred,
			archived: r.ArchivedAt.Valid, deleted: r.DeletedAt.Valid,
			notJunk: r.NotJunk,
		})
	}
	return out
}

func touchedThread(rows []store.SetThreadFlagsRow) []touchedRow {
	out := make([]touchedRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, touchedRow{
			accountID: r.AccountID, folder: r.Folder, uid: r.ImapUid,
			messageID: r.MessageID, read: r.IsRead, starred: r.IsStarred,
			archived: r.ArchivedAt.Valid, deleted: r.DeletedAt.Valid,
			notJunk: r.NotJunk,
		})
	}
	return out
}

// publishFlags queues only the flags this call actually set. Queueing the
// others would publish their current value as though it were a fresh
// decision, and on a mail somebody had just changed in Gmail that would undo
// them.
func publishFlags(ctx context.Context, s *Service, tenantID, ownerID int64, rows []touchedRow, didRead, didStar bool) {
	for _, r := range rows {
		if didRead {
			s.queueFlagWrite(ctx, tenantID, r.accountID, ownerID, r.folder, r.uid, flagSeen, r.read)
		}
		if didStar {
			s.queueFlagWrite(ctx, tenantID, r.accountID, ownerID, r.folder, r.uid, flagFlagged, r.starred)
		}
	}
}

// publishMoves carries deletion, archiving and junk rescue up to the host.
// Separate from the flags because these move the message rather than label
// it, and because only the ones this call actually decided should travel.
func publishMoves(ctx context.Context, s *Service, tenantID, ownerID int64, rows []touchedRow, didArchive, didDelete, didNotJunk bool) {
	for _, r := range rows {
		if didDelete {
			s.queueFolderMove(ctx, tenantID, r.accountID, ownerID, r.folder, r.uid, r.messageID, flagTrash, r.deleted)
		}
		if didArchive {
			s.queueFolderMove(ctx, tenantID, r.accountID, ownerID, r.folder, r.uid, r.messageID, flagArchive, r.archived)
		}
		// Only in one direction. "This is not spam" moves the mail back to the
		// inbox and tells the provider's filter it misjudged the sender;
		// undoing that would mean asking the host to call it spam again, which
		// is not something this screen offers or should.
		if didNotJunk && r.notJunk && r.folder == "JUNK" {
			s.queueFolderMove(ctx, tenantID, r.accountID, ownerID, r.folder, r.uid, r.messageID, flagNotJunk, true)
		}
	}
}

// MarkViewRead marks everything in one view read and reports how many rows
// changed.
//
// Deliberately scoped to the view rather than the whole mailbox: the button
// sits above a list, and it should do what the list shows. Marking the junk
// view read must not silently clear the inbox.
func (s *Service) MarkViewRead(ctx context.Context, tenantID, ownerID int64, view string) (int64, error) {
	view, err := knownView(view)
	if err != nil {
		return 0, err
	}
	touched, err := s.q.MarkViewRead(ctx, store.MarkViewReadParams{
		TenantID: tenantID, OwnerID: ownerID, View: view,
	})
	if err != nil {
		return 0, err
	}
	for _, t := range touched {
		s.queueFlagWrite(ctx, tenantID, t.AccountID, ownerID, t.Folder, t.ImapUid, flagSeen, true)
	}
	return int64(len(touched)), nil
}

// PurgeInbound permanently deletes mail from the caller's trash: the database
// records and their copies in object storage (raw MIME, extracted
// attachments), and the host's copy with them: "彻底删除" that leaves the mail
// sitting in Gmail would not be one. The host deletion is queued before the
// row goes, because the row is where the Message-ID that finds it lives.
//
// wholeThread deletes every trashed message of the conversation, matching
// what the trash list shows: one row per conversation. A live message of the
// same thread is never swept up — only what is already in the trash.
func (s *Service) PurgeInbound(ctx context.Context, tenantID, ownerID, id int64, wholeThread bool) error {
	row, err := s.q.GetInboundForPurge(ctx, store.GetInboundForPurgeParams{
		TenantID: tenantID, OwnerID: ownerID, ID: id,
	})
	if err != nil {
		// Covers "not yours" and "not in the trash" alike: neither is the
		// caller's to distinguish.
		return errNotFound()
	}

	type target struct {
		id        int64
		rawKey    string
		accountID int64
		folder    string
		uid       int64
		messageID string
	}
	targets := []target{{
		id: row.ID, rawKey: row.RawKey, accountID: row.AccountID,
		folder: row.Folder, uid: row.ImapUid, messageID: row.MessageID,
	}}
	if wholeThread {
		full, err := s.q.GetInbound(ctx, store.GetInboundParams{TenantID: tenantID, ID: id})
		if err == nil && full.OwnerID == ownerID && full.ThreadKey != "" {
			// 同上，而且这一条更要紧：后面接的是永久删除加一条发给邮件
			// 服务器的删除指令。另一个信箱里那份同名会话必须留下。
			rows, err := s.q.ListThreadForPurge(ctx, store.ListThreadForPurgeParams{
				TenantID: tenantID, OwnerID: ownerID, AccountID: full.AccountID,
				ThreadKey: full.ThreadKey,
			})
			if err != nil {
				return err
			}
			targets = targets[:0]
			for _, r := range rows {
				targets = append(targets, target{
					id: r.ID, rawKey: r.RawKey, accountID: r.AccountID,
					folder: r.Folder, uid: r.ImapUid, messageID: r.MessageID,
				})
			}
		}
	}

	for _, tg := range targets {
		// Queued before the row goes: the queue row needs the Message-ID, and
		// after the delete there is nowhere left to read it from. Ordered this
		// way, the worst case is an op for a mail the ERP has already
		// forgotten — which still deletes the right message on the host.
		s.queueFolderMove(ctx, tenantID, tg.accountID, ownerID, tg.folder, tg.uid, tg.messageID, flagPurge, true)
		if err := s.purgeOne(ctx, tenantID, ownerID, tg.id, tg.rawKey); err != nil {
			return err
		}
	}
	return nil
}

// purgeOne removes one message's objects and then its row.
//
// Objects first, row last. The row is the retry handle: if an object removal
// fails halfway the mail stays in the trash and a second attempt covers
// whatever remains (removals are idempotent). Deleting the row first would
// leave orphaned objects with nothing pointing at them.
func (s *Service) purgeOne(ctx context.Context, tenantID, ownerID, id int64, rawKey string) error {
	if s.files != nil {
		atts, err := s.q.ListInboundAttachments(ctx, store.ListInboundAttachmentsParams{
			TenantID: tenantID, InboundID: id,
		})
		if err != nil {
			return err
		}
		keys := make([]string, 0, len(atts)+1)
		for _, a := range atts {
			if a.FileKey != "" {
				keys = append(keys, a.FileKey)
			}
		}
		// The pictures we fetched on this mail's behalf. The table row goes
		// with the message through the cascade; the bytes only go if they are
		// named here first.
		keys = append(keys, s.imageKeysFor(ctx, tenantID, id)...)
		if rawKey != "" {
			keys = append(keys, rawKey)
		}
		for _, key := range keys {
			if err := s.files.Remove(ctx, key); err != nil {
				return apierr.Internal("NT_PURGE_STORAGE", "对象存储删除失败，请重试")
			}
		}
	}

	n, err := s.q.PurgeInbound(ctx, store.PurgeInboundParams{
		TenantID: tenantID, OwnerID: ownerID, ID: id,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return errNotFound()
	}
	return nil
}

// SyncNow pulls the caller's mailbox immediately and reports what arrived.
//
// Interactive: it answers once the inbox is in, and the sent folder, the junk
// folder and the read-state reconciliation carry on behind it. Somebody who
// clicks 立即收信 is asking whether the customer has replied, and waiting out
// five further round trips to be told so is the button feeling broken.
//
// accountID 是「收哪个箱」，0 = 默认箱。
//
// **这里从前传的是 employeeID。** 一人一箱的年代 SyncMailboxInteractive 收的
// 就是员工号；第一期把它换成了账号号，而这个调用点漏了——两个都是 int64，
// 编译器一声不吭。于是 立即收信 拿员工号当账号号去查 mail_accounts：查不到
// 就是「邮箱账号不存在」（点了没反应），查得到就是**同事的信箱**——花的是
// 同事的配额，动的是同事的已读状态。
func (s *Service) SyncNow(ctx context.Context, tenantID, employeeID, accountID int64) (int, bool, error) {
	// 先认箱再看有没有邮件通道：「这个箱不是你的」和「你一个箱都没绑」都比
	// 「邮件服务没配」更具体，也更该先说。
	if accountID > 0 {
		if _, err := s.mailboxOfMine(ctx, tenantID, employeeID, accountID); err != nil {
			return 0, false, err
		}
	} else {
		// 没点名：收默认箱。旧前端和只绑了一个箱的人走这条。
		id, err := s.defaultAccountIDFor(ctx, tenantID, employeeID)
		if err != nil {
			return 0, false, ErrNoMailAccount
		}
		accountID = id
	}
	if s.mailbox == nil {
		return 0, false, ErrMailHostNotConfigured
	}
	return s.SyncMailboxInteractive(ctx, SyncConfig{TenantID: tenantID}, accountID)
}

func errNotFound() error {
	return apierr.NotFound("NT_MESSAGE_NOT_FOUND", "邮件记录不存在")
}

// ListMailboxSent is the Sent folder, and it is a folder: every row is a real
// message the host is holding, so starring, archiving and deleting work here
// the same way they do in the inbox. See ListSentUnified for why the ERP's own
// delivery record is joined on rather than listed.
//
// Keyset like every other mailbox list. The cursor carries the kind as well as
// the time and id, because the two halves of the query number their rows in
// different tables and (at, id) alone is not a unique position.
// accountID 是「只看这个信箱发出去的」，0 = 全部。和收件箱一路同一个口径。
//
// sort 见 ListSort；这一侧的列是收件人、主题、日期、大小。和收件箱不同，
// 已发送的搜索和排序可以同时用——它们是同一条查询，不存在两种口径。
func (s *Service) ListMailboxSent(ctx context.Context, tenantID, ownerID, accountID int64, keyword, cursor string, size int32, sort ListSort) (InboundPage, error) {
	_, size = normalizePage(1, size)
	// 已发送没有星标也没有未读，「优先显示」在这一侧不存在。调用方今天传
	// 不进来（proto 里那条请求没有这两个字段），这里再脱一层是因为游标里
	// 带着分档：万一哪天传进来了，对不上的样子是翻第二页翻到别处去。
	sort, err := normalizeListSort(sort.withoutTop(), sentSortColumns)
	if err != nil {
		return InboundPage{}, err
	}
	keyword = strings.TrimSpace(keyword)
	key, kind, id, err := decodeSentSortCursor(cursor, sort)
	if err != nil {
		return InboundPage{}, err
	}
	var acct *int64
	if accountID > 0 {
		acct = &accountID
	}
	rows, err := s.q.ListSentUnified(ctx, store.ListSentUnifiedParams{
		TenantID: tenantID, OwnerID: ownerID, AccountID: acct, Keyword: keyword,
		SortBy: sort.By, SortDir: sort.Dir,
		CursorKey: key, CursorKind: kind, CursorID: id, RowLimit: size,
	})
	if err != nil {
		return InboundPage{}, err
	}
	total, err := s.q.CountSentUnified(ctx, store.CountSentUnifiedParams{
		TenantID: tenantID, OwnerID: ownerID, AccountID: acct, Keyword: keyword,
	})
	if err != nil {
		return InboundPage{}, err
	}
	out := make([]InboundView, 0, len(rows))
	for _, r := range rows {
		v := InboundView{
			ID: r.ID, Kind: r.Kind, ToEmail: r.ToEmail, ToName: r.ToName,
			Subject: r.Subject, Snippet: r.Snippet, Status: r.Status,
			ThreadKey: r.ThreadKey, IsStarred: r.IsStarred,
			// Sent mail is mail you wrote; there is nothing to have not read.
			IsRead: true, HasAttachments: r.HasAttachments,
			// Tracked 必须跟着 OpenedAt 一起走。查询早就把它取出来了，这里
			// 原来漏拷——于是列表行上「没人打开」和「没在看」分不出来，
			// 三态在详情页齐全、在列表上塌成两态。
			Tracked: r.Tracked,
			RawSize: r.RawSize,
			ToAll:   r.ToAll,
		}
		// One timestamp in both fields: the list sorts and displays on "when it
		// went out", and the two halves name that differently.
		if r.At.Valid {
			v.SentAt = r.At.Time
			v.ReceivedAt = r.At.Time
		}
		if r.OpenedAt.Valid {
			v.OpenedAt = r.OpenedAt.Time
		}
		out = append(out, v)
	}
	page := InboundPage{Mails: out, Total: total}
	if int32(len(out)) == size && size > 0 {
		last := rows[len(rows)-1]
		page.NextCursor = encodeSentSortCursor(sort, last.SortKey, last.Kind, last.ID)
	}
	return page, nil
}

// The Sent cursor used to be (time, kind, id). It is now (sort, kind, id,
// sort key) — see encodeSentSortCursor — and this pair is kept only so the
// old shape still decodes; decodeSentSortCursor converts it.
// Opaque on purpose: nothing downstream should do arithmetic on it.
func encodeSentCursor(at time.Time, kind string, id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(
		strconv.FormatInt(at.UTC().UnixMicro(), 10) + ":" + kind + ":" + strconv.FormatInt(id, 10)))
}

func decodeSentCursor(cursor string) (pgtype.Timestamptz, string, int64, error) {
	if cursor == "" {
		return pgtype.Timestamptz{}, "", 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return pgtype.Timestamptz{}, "", 0, errBadCursor()
	}
	micros, rest, ok := strings.Cut(string(raw), ":")
	if !ok {
		return pgtype.Timestamptz{}, "", 0, errBadCursor()
	}
	kind, idPart, ok := strings.Cut(rest, ":")
	if !ok {
		return pgtype.Timestamptz{}, "", 0, errBadCursor()
	}
	us, err := strconv.ParseInt(micros, 10, 64)
	if err != nil {
		return pgtype.Timestamptz{}, "", 0, errBadCursor()
	}
	id, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil {
		return pgtype.Timestamptz{}, "", 0, errBadCursor()
	}
	return pgtype.Timestamptz{Time: time.UnixMicro(us).UTC(), Valid: true}, kind, id, nil
}

// Trash keeps itself. Thirty days matches what Gmail and Outlook do, and the
// number matters less than the promise: deleted mail is recoverable for a
// while and then it is not, without anybody having to remember to tidy up.
const (
	trashRetention    = 30 * 24 * time.Hour
	trashSweepEvery   = 6 * time.Hour
	trashSweepPerPass = 200
)

// EmptyJunk moves everything in the junk view to the trash.
//
// Deliberately the trash and not oblivion. Gmail's "delete all spam now" is
// permanent, but the mail worth finding in a spam folder is the customer
// enquiry the filter got wrong, and that mistake is usually noticed a day
// later. Thirty days in the trash is the difference between losing the order
// and recovering it; anybody who really wants it gone empties the trash.
//
// Mails already rescued with 「这不是垃圾」 are left alone: they stopped being
// junk the moment somebody said so.
func (s *Service) EmptyJunk(ctx context.Context, tenantID, ownerID int64) (int, error) {
	rows, err := s.q.TrashJunkView(ctx, store.TrashJunkViewParams{
		TenantID: tenantID, OwnerID: ownerID,
	})
	if err != nil {
		return 0, err
	}
	// Carried to the host as well, exactly like deleting one by hand — a spam
	// folder emptied here and still full in Gmail would be a chore done twice.
	for _, r := range rows {
		s.queueFolderMove(ctx, tenantID, r.AccountID, ownerID, r.Folder, r.ImapUid, r.MessageID, flagTrash, true)
	}
	if len(rows) > 0 {
		s.log.Info("junk emptied into the trash", "owner", ownerID, "count", len(rows))
	}
	return len(rows), nil
}

// EmptyTrash purges everything in one person's trash.
//
// The same permanent deletion as one mail at a time, applied to the lot: ERP
// row, stored MIME and attachments, and the copy on the mail host. Reports
// how many went so the caller can say something truthful afterwards.
func (s *Service) EmptyTrash(ctx context.Context, tenantID, ownerID int64) (int, error) {
	rows, err := s.q.ListTrashForPurge(ctx, store.ListTrashForPurgeParams{
		TenantID: tenantID, OwnerID: ownerID,
	})
	if err != nil {
		return 0, err
	}
	done := 0
	for _, r := range rows {
		s.queueFolderMove(ctx, tenantID, r.AccountID, ownerID, r.Folder, r.ImapUid, r.MessageID, flagPurge, true)
		if err := s.purgeOne(ctx, tenantID, ownerID, r.ID, r.RawKey); err != nil {
			// One stubborn object must not strand the rest of the trash. The
			// row stays, so the next attempt picks it up again.
			s.log.Warn("could not purge one mail while emptying the trash",
				"id", r.ID, "err", err)
			continue
		}
		done++
	}
	return done, nil
}

// RunTrashSweeper clears out trash that has sat long enough.
//
// Runs on a slow tick — this is housekeeping, not a deadline. Each pass is
// bounded so a mailbox with years of deleted mail cannot monopolise the
// service on the first run after an upgrade.
// 不再收 SyncConfig：改成按公司遍历之后，这个循环用的全是本文件里的常量
// （trashRetention / trashSweepEvery），配置一项都不读。留着一个被静默忽略的
// 参数，和它原来携带的那个 bug 是同一种东西——看着有用，实际没用。
func (s *Service) RunTrashSweeper(ctx context.Context) {
	s.log.Info("trash sweeper started", "keep", trashRetention, "every", trashSweepEvery)

	t := time.NewTicker(trashSweepEvery)
	defer t.Stop()
	for {
		for _, tenantID := range s.tenantsToServe(ctx) {
			s.sweepTrashOnce(ctx, tenantID)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) sweepTrashOnce(ctx context.Context, tenantID int64) {
	cutoff := time.Now().Add(-trashRetention)
	rows, err := s.q.ListExpiredTrash(ctx, store.ListExpiredTrashParams{
		TenantID: tenantID,
		Cutoff:   pgtype.Timestamptz{Time: cutoff, Valid: true},
		RowLimit: trashSweepPerPass,
	})
	if err != nil {
		s.log.Error("could not list expired trash", "err", err)
		return
	}
	if len(rows) == 0 {
		return
	}
	done := 0
	for _, r := range rows {
		s.queueFolderMove(ctx, tenantID, r.AccountID, r.OwnerID, r.Folder, r.ImapUid, r.MessageID, flagPurge, true)
		if err := s.purgeOne(ctx, tenantID, r.OwnerID, r.ID, r.RawKey); err != nil {
			s.log.Warn("could not sweep one trashed mail", "id", r.ID, "err", err)
			continue
		}
		done++
	}
	s.log.Info("trash swept", "deleted", done, "older_than", trashRetention)
}

// touchMailboxRead 记一笔「有人在看这个箱」。
//
// 尽力而为：这是排班用的时间戳，不是账。写不上最多让这个箱下一轮排得靠后，
// 而让收件箱因此打不开是荒唐的。半分钟内只落一次，限频在 SQL 的 WHERE 里。
func (s *Service) touchMailboxRead(ctx context.Context, tenantID, accountID int64) {
	if err := s.q.TouchMailboxRead(ctx, store.TouchMailboxReadParams{
		TenantID: tenantID, ID: accountID,
	}); err != nil {
		s.log.Warn("could not record mailbox read time", "account", accountID, "err", err)
	}
}

// normalizeView 把请求里的 view 收口到已知的几档。认不得的回落到收件箱：
// 一个坏参数最多只能让人看到默认那一片。自建文件夹是 'F:' 加服务器名，
// 由 mail_view_of 生成、前端原样传回，这里放行。
func normalizeView(view string) string {
	switch {
	case view == "STARRED", view == "ARCHIVE", view == "TRASH", view == "JUNK":
		return view
	case strings.HasPrefix(view, "F:") && len(view) > 2:
		return view
	default:
		return "INBOX"
	}
}

// knownView 是 normalizeView 的严格版，给写操作用。列表认不得视图回落到
// 收件箱，最多让人看到默认那一片；「全部已读」要是也回落，就会把真正收件箱
// 的未读全标掉——前端传错一个键（比如 undefined）就是这个后果。所以这里
// 认不得就拒绝，一封都不动。
func knownView(view string) (string, error) {
	if normalizeView(view) == view {
		return view, nil
	}
	return "", apierr.Invalid("MAIL_VIEW_UNKNOWN", "不认识的视图："+view)
}

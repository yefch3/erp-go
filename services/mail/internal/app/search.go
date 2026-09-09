package app

import (
	"context"
	"strings"

	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// SearchHit is one message found by a search, wherever it was filed.
type SearchHit struct {
	InboundView
	// Folder is why this is a search result and not a list row: the person
	// searched without saying where, so the answer has to say where.
	Folder string
	// AccountID 是同一个理由再往上一层：搜索现在也横跨信箱，所以「在哪个箱」
	// 和「在哪个文件夹」一样，属于答案的一部分。
	AccountID int64
	// MatchSnippet is the text around the hit, not the opening of the mail.
	// The two differ exactly when the search was worth doing — a match in the
	// fourth paragraph is invisible in a snippet of the first.
	MatchSnippet string
}

// SearchPage is one screenful of hits.
type SearchPage struct {
	Hits       []SearchHit
	Total      int64
	NextCursor string
}

// SearchMailMinKeyword is the shortest query that runs.
//
// One character matches most of the mailbox, which is not a search result, it
// is the mailbox with extra steps. The floor is on characters rather than
// bytes because one Chinese character is a word's worth of meaning and three
// bytes, and counting bytes would let 三 through while refusing "ab".
const SearchMailMinKeyword = 2

// SearchMail looks through everything the caller can see, in every folder of
// every mailbox it was handed.
//
// Junk and trash are left out, as Gmail leaves them out: both hold mail the
// person already decided against, and mixing it into results makes every
// search something to be double-checked. See the query for the exception.
//
// accountIDs 是「搜这些箱」，空 = 不限（一个箱都没绑的人）。范围由**网关**
// 定，而且只放进解锁令牌验过的那些——这里不做鉴权，也不该做：它拿到的是
// 一串号码，分不出哪些是验过的。
//
// 从「只搜当前这个箱」改回横跨，理由见查询上的那段注释：不知道东西在哪个箱，
// 正是搜索存在的原因。
func (s *Service) SearchMail(ctx context.Context, tenantID, ownerID int64, accountIDs []int64, keyword, cursor string, size int32) (SearchPage, error) {
	keyword = strings.TrimSpace(keyword)
	if len([]rune(keyword)) < SearchMailMinKeyword {
		// Not an error: the box is being typed into. An empty page with a
		// zero total says "nothing yet" without a red banner over one
		// keystroke.
		return SearchPage{}, nil
	}
	_, size = normalizePage(1, size)

	at, id, err := decodeCursor(cursor)
	if err != nil {
		return SearchPage{}, err
	}
	// nil 和空切片在 pgx 那儿不是一回事：nil 会当成 SQL NULL 送出去，而
	// cardinality(NULL) 是 NULL，不是 0——于是那个「空 = 不限」的判断整条
	// 变成 NULL，一行都回不来。空搜索不报错、只是永远空着，是最难查的那种。
	if accountIDs == nil {
		accountIDs = []int64{}
	}

	rows, err := s.q.SearchMail(ctx, store.SearchMailParams{
		TenantID: tenantID, OwnerID: ownerID, AccountIds: accountIDs, Keyword: keyword,
		CursorAt: at, CursorID: id, RowLimit: size,
	})
	if err != nil {
		return SearchPage{}, err
	}
	total, err := s.q.CountSearchMail(ctx, store.CountSearchMailParams{
		TenantID: tenantID, OwnerID: ownerID, AccountIds: accountIDs, Keyword: keyword,
	})
	if err != nil {
		return SearchPage{}, err
	}

	out := make([]SearchHit, 0, len(rows))
	for _, r := range rows {
		h := SearchHit{
			Folder:       r.Folder,
			AccountID:    r.AccountID,
			MatchSnippet: r.MatchSnippet,
			InboundView: InboundView{
				ID: r.ID, FromEmail: r.FromEmail, FromName: r.FromName,
				ToEmail: r.ToEmail, Subject: r.Subject, ThreadKey: r.ThreadKey,
				IsRead: r.IsRead, IsStarred: r.IsStarred,
				HasAttachments: r.HasAttachments,
			},
		}
		if r.ReceivedAt.Valid {
			h.ReceivedAt = r.ReceivedAt.Time
		}
		if r.SentAt.Valid {
			h.SentAt = r.SentAt.Time
		}
		out = append(out, h)
	}

	page := SearchPage{Hits: out, Total: total}
	if int32(len(out)) == size && size > 0 {
		last := out[len(out)-1]
		page.NextCursor = encodeCursor(last.ReceivedAt, last.ID)
	}
	return page, nil
}

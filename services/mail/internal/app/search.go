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

// SearchMail looks through everything the caller can see in one mailbox, in
// every folder of it.
//
// Junk and trash are left out, as Gmail leaves them out: both hold mail the
// person already decided against, and mixing it into results makes every
// search something to be double-checked. See the query for the exception.
//
// accountID 是「只搜这个箱」，0 = 全部信箱。**按邮箱分**这条口径从前在搜索
// 这个门上漏了：站在 Gmail 箱里搜一个词，263 箱的信也混在结果里——而列表
// 明明只列 Gmail 的。切到哪个箱，搜的就是哪个箱。
func (s *Service) SearchMail(ctx context.Context, tenantID, ownerID, accountID int64, keyword, cursor string, size int32) (SearchPage, error) {
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
	var acct *int64
	if accountID > 0 {
		acct = &accountID
	}

	rows, err := s.q.SearchMail(ctx, store.SearchMailParams{
		TenantID: tenantID, OwnerID: ownerID, AccountID: acct, Keyword: keyword,
		CursorAt: at, CursorID: id, RowLimit: size,
	})
	if err != nil {
		return SearchPage{}, err
	}
	total, err := s.q.CountSearchMail(ctx, store.CountSearchMailParams{
		TenantID: tenantID, OwnerID: ownerID, AccountID: acct, Keyword: keyword,
	})
	if err != nil {
		return SearchPage{}, err
	}

	out := make([]SearchHit, 0, len(rows))
	for _, r := range rows {
		h := SearchHit{
			Folder:       r.Folder,
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

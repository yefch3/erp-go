package app

import (
	"context"
	"strings"
	"time"

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

// SearchMail looks through everything the caller can see, in every folder.
//
// Junk and trash are left out, as Gmail leaves them out: both hold mail the
// person already decided against, and mixing it into results makes every
// search something to be double-checked. See the query for the exception.
func (s *Service) SearchMail(ctx context.Context, tenantID, ownerID int64, keyword, cursor string, size int32) (SearchPage, error) {
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

	rows, err := s.q.SearchMail(ctx, store.SearchMailParams{
		TenantID: tenantID, OwnerID: ownerID, Keyword: keyword,
		CursorAt: at, CursorID: id, RowLimit: size,
	})
	if err != nil {
		return SearchPage{}, err
	}
	total, err := s.q.CountSearchMail(ctx, store.CountSearchMailParams{
		TenantID: tenantID, OwnerID: ownerID, Keyword: keyword,
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

// searchBackfillBatch is how many bodies are pulled into memory at once. Small
// because a body can be megabytes and there is no hurry: nothing waits on
// this, and a mailbox that takes a minute to become searchable is not a
// mailbox anybody is searching in that minute.
const searchBackfillBatch = 200

// BackfillSearchText fills the column for mail stored before it existed.
//
// In Go rather than in the migration, using the same function ingest uses.
// The rows that need it are precisely the HTML-only ones, which is where a
// regexp approximation of HTMLToText would differ from the real thing — and
// two implementations of "what does this mail say" drifting apart is how a
// search comes to find a message by one route and not another.
//
// Idempotent, and safe to run on every start: it selects only rows that are
// still empty and have a body to derive from. Once done, the query matches
// nothing and costs one indexless-but-tiny scan per boot.
func (s *Service) BackfillSearchText(ctx context.Context, cfg SyncConfig) {
	cfg = cfg.withDefaults() // same as every other background job: tenant 1 unless told
	tenantID := cfg.TenantID
	started := time.Now()
	filled := 0
	for {
		rows, err := s.q.ListInboundNeedingSearchText(ctx, store.ListInboundNeedingSearchTextParams{
			TenantID: tenantID, RowLimit: searchBackfillBatch,
		})
		if err != nil {
			s.log.Warn("search backfill could not read a batch", "err", err)
			return
		}
		if len(rows) == 0 {
			break
		}
		before := filled
		for _, r := range rows {
			text := searchTextOf(r.Subject, r.FromName, r.FromEmail, r.ToEmail, r.BodyText, r.BodyHtml)
			if text == "" {
				// A body that renders to nothing — an image-only mail. Storing
				// a single space stops this row coming back every batch and
				// spinning the loop for ever; it is not searchable either way.
				text = " "
			}
			if err := s.q.SetSearchText(ctx, store.SetSearchTextParams{
				TenantID: tenantID, ID: r.ID, SearchText: text,
			}); err != nil {
				s.log.Warn("search backfill could not write a row", "id", r.ID, "err", err)
				continue
			}
			filled++
		}
		if filled == before {
			// Every row in the batch failed to write. Without this the outer
			// loop would fetch the same batch for ever.
			s.log.Warn("search backfill made no progress, stopping", "remaining", len(rows))
			return
		}
	}
	if filled > 0 {
		s.log.Info("search text backfilled", "rows", filled, "took", time.Since(started).String())
	}
}

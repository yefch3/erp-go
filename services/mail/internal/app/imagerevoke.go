package app

import (
	"context"
	"regexp"

	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// C28 的最后一条尾巴: an image a deleted signature was the last user of must
// not stay reachable on the public internet.
//
// Gallery images are served login-free by design — the fetcher is the
// recipient's mail client, which has no session, so the random token is the
// whole credential. That design has a debt: when the signature that embedded
// an image is deleted, nothing used to take the image down, and a URL nobody
// remembers is still a URL anybody who ever received it can open.
//
// The sweep runs where the references change — signature and template
// deletes and edits — and withdraws a departed image only when NOTHING else
// references it. The reference count is not maintained anywhere; it is
// counted at the moment of the question, in one SQL statement, across every
// surface an image can be embedded in (signatures, templates, drafts,
// campaign bodies, and — decisively — sent messages, which reference their
// images forever). Deletes and edits are rare and the tables are small;
// a count that cannot drift beats a counter that must be kept right.

var mailImageTokenPattern = regexp.MustCompile(`/api/public/mail-images/([A-Za-z0-9]{16,64})`)

func imageTokensIn(html string) map[string]bool {
	out := map[string]bool{}
	for _, m := range mailImageTokenPattern.FindAllStringSubmatch(html, -1) {
		out[m[1]] = true
	}
	return out
}

// sweepRemovedImages withdraws every image that `before` referenced and
// `after` no longer does, unless something else still uses it. Best-effort
// by design: the delete or edit that triggered it has already succeeded, and
// a sweep hiccup must not undo that — a missed orphan is one more unused
// URL, exactly the state everything was in before this file existed.
func (s *Service) sweepRemovedImages(ctx context.Context, tenantID int64, before, after string) {
	kept := imageTokensIn(after)
	for token := range imageTokensIn(before) {
		if kept[token] {
			continue
		}
		n, err := s.q.WithdrawImageIfOrphaned(ctx, store.WithdrawImageIfOrphanedParams{
			TenantID: tenantID, Token: token,
		})
		if err != nil {
			s.log.Warn("could not sweep a possibly-orphaned image", "token", token, "err", err)
			continue
		}
		if n > 0 {
			// The object itself stays, same as a manual withdrawal: serving
			// stops, bytes remain. See WithdrawImage for the reasoning.
			s.log.Info("image withdrawn — its last reference was just removed", "token", token)
		}
	}
}

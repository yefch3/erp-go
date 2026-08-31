package app

import (
	"context"
	"time"

	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// How many originals are held in memory at once. Sent mail carries
// attachments, so a message can be tens of megabytes; the pass is in no
// hurry, the bound is what matters.
const refetchBatch = 8

// RunRawOriginalRefetch goes back to the mail host for the originals the
// raw-key collision destroyed. The collision repair blanked the rows that
// were pointing at somebody else's mail; this pass is the other half of the
// cure it promised — the messages themselves usually still exist on the
// host, and a copy fetched today is as much the original as the one lost.
//
// Each message is found by Message-ID, not by the row's stored UID. The
// stored UID names a message only within the folder generation it was read
// from, and trusting it across generations is precisely the mistake that
// lost these originals. The Message-ID travels with the message; what it
// finds is re-keyed for the folder's current generation and current UID, so
// the recovered object is correct under the new scheme by construction.
//
// A message the host no longer has — spam past its retention, mail deleted
// in another client — is left as it is and said out loud once. The row keeps
// raw_key = ” and every repair pass keeps skipping it, which is the honest
// state: no original, and no pretending otherwise.
//
// Runs last in the repair chain, after the collision repair has finished
// disowning: a row must be known to have no original before it is worth
// going back to the host for one. Safe to run repeatedly — recovery fills
// raw_key, and the query stops offering the row.
func (s *Service) RunRawOriginalRefetch(ctx context.Context, cfg SyncConfig) {
	cfg = cfg.withDefaults()
	if s.mailbox == nil || s.files == nil {
		return
	}
	rows, err := s.q.ListInboundMissingRaw(ctx, cfg.TenantID)
	if err != nil {
		s.log.Warn("raw refetch could not list the rows missing an original", "err", err)
		return
	}
	if len(rows) == 0 {
		return
	}

	// Grouped by mailbox owner: credentials are per employee, and one
	// mailbox that cannot be opened must not stop the others recovering.
	// 按账号分，不按人分：重取要回到这封信当初进来的那个信箱。
	byAccount := map[int64][]store.ListInboundMissingRawRow{}
	var accounts []int64
	for _, r := range rows {
		if _, seen := byAccount[r.AccountID]; !seen {
			accounts = append(accounts, r.AccountID)
		}
		byAccount[r.AccountID] = append(byAccount[r.AccountID], r)
	}
	s.log.Info("raw refetch starting", "messages", len(rows), "mailboxes", len(accounts))

	recovered, gone, failed := 0, 0, 0
	for _, accountID := range accounts {
		if ctx.Err() != nil {
			return
		}
		acct, err := s.ForAccount(ctx, cfg.TenantID, accountID)
		if err != nil {
			s.log.Warn("raw refetch could not open a mailbox, leaving its rows for next time",
				"account", accountID, "rows", len(byAccount[accountID]), "err", err)
			failed += len(byAccount[accountID])
			continue
		}
		r, g, f := s.refetchForAccount(ctx, cfg.TenantID, acct, byAccount[accountID])
		recovered, gone, failed = recovered+r, gone+g, failed+f
	}
	s.log.Info("raw refetch finished",
		"recovered", recovered, "no longer on host", gone, "failed", failed)
}

// refetchForAccount recovers one mailbox's lost originals. Every row lands
// in exactly one of the three counts: recovered (original back and adopted),
// gone (the host no longer has the message), failed (anything in between —
// worth retrying on a later pass).
func (s *Service) refetchForAccount(ctx context.Context, tenantID int64, acct MailAccount,
	rows []store.ListInboundMissingRawRow) (recovered, gone, failed int) {

	byFolder := map[string][]store.ListInboundMissingRawRow{}
	var folders []string
	for _, r := range rows {
		if _, seen := byFolder[r.Folder]; !seen {
			folders = append(folders, r.Folder)
		}
		byFolder[r.Folder] = append(byFolder[r.Folder], r)
	}

	for _, folder := range folders {
		group := byFolder[folder]
		if ctx.Err() != nil {
			failed += len(group)
			continue
		}
		host, err := s.hostFolder(ctx, acct, folder)
		if err != nil {
			s.log.Warn("raw refetch could not name a folder on the host",
				"folder", folder, "err", err)
			failed += len(group)
			continue
		}

		// One row per Message-ID. A duplicate means the same message was
		// ingested twice into one folder; the first row takes the recovery
		// and the rest wait for a later pass, when the query no longer
		// offers the recovered one.
		byMID := map[string]store.ListInboundMissingRawRow{}
		var ids []string
		for _, r := range group {
			if _, dup := byMID[r.MessageID]; dup {
				failed++
				continue
			}
			byMID[r.MessageID] = r
			ids = append(ids, r.MessageID)
		}

		found, err := s.mailbox.FindUIDsByMessageIDs(ctx, acct, host, ids)
		if err != nil {
			s.log.Warn("raw refetch could not search a folder",
				"folder", folder, "err", err)
			failed += len(ids)
			continue
		}

		var uids []uint32
		for _, id := range ids {
			if uid, ok := found[id]; ok {
				uids = append(uids, uid)
			}
		}
		claimed := s.refetchClaim(ctx, tenantID, acct, folder, host, uids, byMID)

		recovered += claimed
		gone += len(ids) - len(found)
		failed += len(found) - claimed
		if n := len(ids) - len(found); n > 0 {
			s.log.Info("raw refetch: messages no longer on the host",
				"folder", folder, "count", n)
		}
	}
	return recovered, gone, failed
}

// refetchClaim fetches the named UIDs in bounded batches and adopts each
// message for the row whose Message-ID it actually carries. The parsed
// Message-ID is the deciding match — not the UID the search reported a
// moment ago, because between the search and the fetch the host is free to
// renumber, and adopting by UID under a renumbering writes a stranger's mail
// into the row. That corruption is what this repair family exists to undo,
// so refusing is always the right failure.
func (s *Service) refetchClaim(ctx context.Context, tenantID int64, acct MailAccount,
	folder, host string, uids []uint32, byMID map[string]store.ListInboundMissingRawRow) (claimed int) {

	for start := 0; start < len(uids); start += refetchBatch {
		if ctx.Err() != nil {
			return claimed
		}
		end := min(start+refetchBatch, len(uids))
		res, err := s.mailbox.FetchByUIDs(ctx, acct, host, uids[start:end])
		if err != nil {
			s.log.Warn("raw refetch could not fetch a batch", "folder", folder, "err", err)
			continue
		}
		if res.UIDValidity == 0 {
			// A key with validity 0 would collide across generations the same
			// way the old scheme did. No host this service talks to omits it;
			// refusing the batch is cheaper than a second incident.
			s.log.Warn("raw refetch: host reported no UIDVALIDITY, refusing the batch",
				"folder", folder)
			continue
		}

		for _, m := range res.Messages {
			parsed, err := ParseMail(m.Raw)
			if err != nil || parsed.MessageID == "" {
				s.log.Warn("raw refetch fetched a message it could not identify",
					"folder", folder, "uid", m.UID, "err", err)
				continue
			}
			row, ok := byMID[parsed.MessageID]
			if !ok {
				s.log.Warn("raw refetch fetched a message nobody here asked for, refusing it",
					"folder", folder, "uid", m.UID)
				continue
			}

			key := rawKeyFor(tenantID, acct.AccountID, folder, res.UIDValidity, m.UID)
			if err := s.putRaw(ctx, key, m.Raw); err != nil {
				s.log.Warn("raw refetch could not store a recovered original",
					"folder", folder, "uid", m.UID, "err", err)
				continue
			}
			n, err := s.q.AdoptInboundRawKey(ctx, store.AdoptInboundRawKeyParams{
				TenantID: tenantID, ID: row.ID, RawKey: key, RawSize: int64(len(m.Raw)),
			})
			if err != nil {
				s.log.Warn("raw refetch could not adopt a recovered original",
					"id", row.ID, "err", err)
				continue
			}
			if n == 0 {
				// Somebody filled the row between the listing and now. Their
				// key stands; the object written above is orphaned, which
				// storage lifecycle can sweep — better an unreferenced object
				// than a row whose key was overwritten underneath a reader.
				continue
			}
			claimed++
			delete(byMID, parsed.MessageID)
		}

		// The pass runs beside live mail sync and nothing waits on it.
		select {
		case <-ctx.Done():
			return claimed
		case <-time.After(50 * time.Millisecond):
		}
	}
	return claimed
}

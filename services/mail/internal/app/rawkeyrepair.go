package app

import (
	"context"
	"time"

	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// RunRawKeyCollisionRepair settles which message each shared original belongs
// to, and takes the original away from the messages it does not.
//
// Until 2026-08-12 a message's MIME was stored under tenant/account/uid. A UID
// is unique within a folder, not within an account, so INBOX 614, SENT 614 and
// JUNK 614 all wrote to one object and whichever synced last overwrote the
// others. See rawKeyFor for why the key is what it is now.
//
// New mail is keyed correctly. This pass is about the rows already written,
// and the damage they carry is worse than a missing file: each of them still
// points at an object as though it were its own original, and every repair
// pass in this service re-parses from there. Left alone, the next repair that
// touches one of these writes a stranger's mail into it.
//
// So the object is read once per collision group and its Message-ID decides
// the owner. The owner keeps the key; everybody else has it blanked. Nothing
// is deleted - the surviving object is somebody's genuine original.
//
// What this pass does not do is get the lost originals back. They were
// overwritten in object storage and are gone from here; the messages usually
// still exist on the mail host, so re-fetching them is possible and is a
// separate piece of work. Blanking is the safety fix, not the whole cure.
//
// Safe to run repeatedly: a group whose losers have been blanked no longer has
// more than one row per key, so it stops being selected.
func (s *Service) RunRawKeyCollisionRepair(ctx context.Context, cfg SyncConfig) {
	cfg = cfg.withDefaults()
	if s.files == nil {
		return
	}
	rows, err := s.q.ListCollidingRawMessages(ctx, cfg.TenantID)
	if err != nil {
		s.log.Warn("raw-key repair could not read the collisions", "err", err)
		return
	}
	if len(rows) == 0 {
		return
	}

	groups, order := groupByRawKey(rows)
	s.log.Info("raw-key repair starting", "messages", len(rows), "shared originals", len(order))

	disowned, unresolved := 0, 0
	for _, key := range order {
		if ctx.Err() != nil {
			return
		}
		n, ok := s.settleRawKeyGroup(ctx, cfg.TenantID, key, groups[key])
		disowned += n
		if !ok {
			unresolved++
		}
		// The originals are read one at a time and this runs beside live mail
		// sync; there is nothing waiting on it finishing sooner.
		select {
		case <-ctx.Done():
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
	s.log.Info("raw-key repair finished",
		"originals disowned", disowned, "groups nobody claimed", unresolved)
}

func groupByRawKey(rows []store.ListCollidingRawMessagesRow) (map[string][]store.ListCollidingRawMessagesRow, []string) {
	groups := map[string][]store.ListCollidingRawMessagesRow{}
	var order []string
	for _, r := range rows {
		if _, seen := groups[r.RawKey]; !seen {
			order = append(order, r.RawKey)
		}
		groups[r.RawKey] = append(groups[r.RawKey], r)
	}
	return groups, order
}

// settleRawKeyGroup reads one shared original and disowns the rows it is not.
// Returns how many rows were disowned and whether an owner was identified.
func (s *Service) settleRawKeyGroup(ctx context.Context, tenantID int64, key string,
	rows []store.ListCollidingRawMessagesRow) (disowned int, resolved bool) {

	raw, err := s.readRaw(ctx, key)
	if err != nil {
		// The object is unreadable, so it is nobody's original and every row
		// claiming it is claiming something that is not there.
		s.log.Warn("raw-key repair could not read a shared original, disowning all of it",
			"key", key, "rows", len(rows), "err", err)
		for _, r := range rows {
			if s.clearRawKey(ctx, tenantID, r.ID) {
				disowned++
			}
		}
		return disowned, false
	}

	parsed, err := ParseMail(raw)
	if err != nil {
		s.log.Warn("raw-key repair could not parse a shared original", "key", key, "err", err)
		return 0, false
	}

	// The Message-ID settles it. It is the one identifier that travels with the
	// message rather than being assigned by the folder it landed in, which is
	// exactly what the old key got wrong.
	owner := int64(0)
	if parsed.MessageID != "" {
		for _, r := range rows {
			if r.MessageID == parsed.MessageID {
				owner = r.ID
				break
			}
		}
	}
	if owner == 0 {
		// The object belongs to a message that is not in this group at all -
		// it was overwritten again by a row since deleted, or the original had
		// no Message-ID to match on. Nobody may claim it.
		s.log.Warn("raw-key repair found an original nobody in the group claims",
			"key", key, "rows", len(rows))
	}

	for _, r := range rows {
		if r.ID == owner {
			continue
		}
		if s.clearRawKey(ctx, tenantID, r.ID) {
			disowned++
		}
	}
	return disowned, owner != 0
}

func (s *Service) clearRawKey(ctx context.Context, tenantID, id int64) bool {
	n, err := s.q.ClearInboundRawKey(ctx, store.ClearInboundRawKeyParams{
		TenantID: tenantID, ID: id,
	})
	if err != nil {
		s.log.Warn("raw-key repair could not disown an original", "id", id, "err", err)
		return false
	}
	return n > 0
}

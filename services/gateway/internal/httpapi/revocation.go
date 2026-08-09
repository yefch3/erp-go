package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/sgao19/erp-go/pkg/grpcx"
)

// Ending one person's sessions without ending everybody's.
//
// A login token is a bearer token: it proves somebody authenticated once, and
// after that it works from anywhere until it expires. There was exactly one
// way to take one back — change JWT_SECRET, which invalidates every token in
// the company. So "this person left today" and "this laptop was stolen" both
// cost a company-wide sign-out, which is the sort of remedy nobody reaches
// for at 3pm on a Tuesday. In practice it meant the answer was to wait a day.
//
// The record here is one timestamp per person: every token issued to them
// before it is dead. Not a list of individual tokens — a token has no
// identifier worth storing, and the useful instruction is always "everything
// they hold right now", never "that one specific session".
//
// Marking somebody as left already stops them logging in again (see the
// status check in iam's Login). This is the other half: it stops the session
// they are already holding. The two are different questions and both need
// answering — one closes the door, the other clears the room.

const (
	// One hash per tenant rather than a key per person: a revocation is rare
	// and the whole set is small, so it is read in a single round trip.
	revocationKeyPrefix = "erp.revoked.t"
	// How often the snapshot is rebuilt. Ten seconds is the longest a
	// revoked session can outlive the instruction — short enough to be
	// honest about on screen, long enough that this costs nothing.
	revocationRefresh = 10 * time.Second
	// Entries older than this are pruned: every token issued before them has
	// expired on its own, so remembering the revocation proves nothing.
	// Comfortably longer than JWT_TTL, which defaults to 24 hours.
	revocationRetention = 72 * time.Hour
)

// RevocationStore answers "is this token older than the last time somebody
// pulled the plug on its owner".
//
// Reads come from an in-memory snapshot, not from Redis. Two reasons, and the
// second is the important one.
//
// Speed: this runs on every authenticated request, and a network round trip
// per request to answer "no" for everybody is a poor trade.
//
// And the failure mode. Asking Redis per request means deciding what to do
// when Redis is unreachable, and both answers are bad: fail open and a
// revoked session comes back to life exactly when the infrastructure is
// already having a bad day; fail closed and a Redis blip signs out the whole
// company, which is a self-inflicted version of the outage this feature
// exists to avoid. A snapshot has a third answer — keep enforcing what was
// last known to be true. Revocations issued during the outage do not take
// effect until it ends; everything decided before it stays enforced.
type RevocationStore struct {
	rdb *redis.Client
	log *slog.Logger
	// Read on every request, replaced wholesale by the refresher. atomic.Value
	// rather than a mutex because the read side is hot and never writes.
	snapshot atomic.Value // map[string]int64, keyed by "t<tenant>.e<employee>"
	// When the snapshot was last successfully rebuilt, so staleness is
	// visible rather than silent.
	loadedAt atomic.Int64
}

func NewRevocationStore(addr string, log *slog.Logger) *RevocationStore {
	s := &RevocationStore{
		rdb: redis.NewClient(&redis.Options{Addr: addr}),
		log: log,
	}
	s.snapshot.Store(map[string]int64{})
	return s
}

func revocationKey(tenantID int64) string {
	return revocationKeyPrefix + strconv.FormatInt(tenantID, 10)
}

func snapshotKey(tenantID, employeeID int64) string {
	return fmt.Sprintf("t%d.e%d", tenantID, employeeID)
}

// Revoke ends every session this person currently holds.
//
// The timestamp is now: tokens issued before this instant are dead, tokens
// issued after it are not. A token minted in the same second survives, which
// is correct — revocation ends the sessions that exist, it does not prevent
// somebody with valid credentials from signing in again. Stopping that is
// what disabling the account does, and the two are deliberately separate.
func (s *RevocationStore) Revoke(ctx context.Context, tenantID, employeeID int64) error {
	now := time.Now().Unix()
	key := revocationKey(tenantID)
	if err := s.rdb.HSet(ctx, key, strconv.FormatInt(employeeID, 10), now).Err(); err != nil {
		return err
	}
	// The hash as a whole outlives any token that could be affected by it.
	// Refreshed on every write so an active tenant never loses its entries.
	if err := s.rdb.Expire(ctx, key, revocationRetention).Err(); err != nil {
		s.log.Warn("could not set an expiry on the revocation list", "err", err)
	}
	// Applied to this replica at once rather than waiting for the next
	// refresh. The administrator who just pressed the button should see it
	// take effect, and any other replica catches up within the interval.
	s.applyLocally(tenantID, employeeID, now)
	return nil
}

func (s *RevocationStore) applyLocally(tenantID, employeeID, at int64) {
	cur, _ := s.snapshot.Load().(map[string]int64)
	next := make(map[string]int64, len(cur)+1)
	for k, v := range cur {
		next[k] = v
	}
	next[snapshotKey(tenantID, employeeID)] = at
	s.snapshot.Store(next)
}

// Revoked reports whether a token issued at this instant is still good.
//
// issuedAt is the token's own claim. A token with no issued-at is treated as
// revoked whenever its owner has ever been revoked: a token that cannot say
// when it was minted cannot be shown to predate anything.
func (s *RevocationStore) Revoked(tenantID, employeeID int64, issuedAt time.Time) bool {
	snap, _ := s.snapshot.Load().(map[string]int64)
	at, ok := snap[snapshotKey(tenantID, employeeID)]
	if !ok {
		return false
	}
	if issuedAt.IsZero() {
		return true
	}
	return issuedAt.Unix() < at
}

// Run keeps the snapshot fresh until the context ends.
func (s *RevocationStore) Run(ctx context.Context) {
	// Once immediately, so a gateway that has just restarted is not briefly
	// honouring sessions somebody revoked yesterday.
	s.refresh(ctx)
	t := time.NewTicker(revocationRefresh)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.refresh(ctx)
		}
	}
}

// refresh rebuilds the snapshot, or keeps the old one if it cannot.
//
// Keeping the old one is the whole point of holding a snapshot at all: an
// unreachable Redis must not quietly un-revoke anybody, and must not sign out
// people who were never revoked.
func (s *RevocationStore) refresh(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// Tenants are not enumerated: there is one today and the multi-tenant
	// story is a reserved column, not a live feature (see the architecture
	// note on multi-tenancy). Scanning the prefix keeps this correct on the
	// day that changes without inventing a registry now.
	keys, err := s.rdb.Keys(ctx, revocationKeyPrefix+"*").Result()
	if err != nil {
		s.log.Error("could not read the revocation list; keeping the last known one",
			"age", s.age().Round(time.Second).String(), "err", err)
		return
	}
	next := map[string]int64{}
	cutoff := time.Now().Add(-revocationRetention).Unix()
	for _, key := range keys {
		tenantID, convErr := strconv.ParseInt(key[len(revocationKeyPrefix):], 10, 64)
		if convErr != nil {
			continue
		}
		entries, hErr := s.rdb.HGetAll(ctx, key).Result()
		if hErr != nil {
			s.log.Error("could not read a tenant's revocation list; keeping the last known one",
				"tenant", tenantID, "err", hErr)
			return
		}
		for field, value := range entries {
			employeeID, e1 := strconv.ParseInt(field, 10, 64)
			at, e2 := strconv.ParseInt(value, 10, 64)
			if e1 != nil || e2 != nil || at < cutoff {
				// Older than any token that could still be alive. Dropped
				// from the snapshot; Redis expires the hash on its own.
				continue
			}
			next[snapshotKey(tenantID, employeeID)] = at
		}
	}
	s.snapshot.Store(next)
	s.loadedAt.Store(time.Now().Unix())
}

func (s *RevocationStore) age() time.Duration {
	at := s.loadedAt.Load()
	if at == 0 {
		return 0
	}
	return time.Since(time.Unix(at, 0))
}

func (s *RevocationStore) Close() error { return s.rdb.Close() }

// revokeSessions is the administrator's button: this person's current logins
// stop working.
//
// Separate from marking them as left, because the two are wanted separately —
// a stolen laptop is not a resignation — and joined where it matters:
// deactivating an employee calls this as well, since somebody who may not log
// in again should not still be logged in.
func (s *Server) revokeSessions(w http.ResponseWriter, r *http.Request) {
	op, _ := grpcx.OperatorFromContext(r.Context())
	id := idFromPath(r)
	if s.Revocations == nil {
		s.writeError(w, http.StatusServiceUnavailable, "AUTH_REVOKE_UNAVAILABLE",
			"会话吊销暂不可用，请稍后再试")
		return
	}
	if err := s.Revocations.Revoke(r.Context(), op.TenantID, id); err != nil {
		s.Log.Error("could not revoke sessions", "employee", id, "err", err)
		s.writeError(w, http.StatusInternalServerError, "AUTH_REVOKE_FAILED",
			"吊销失败，请重试")
		return
	}
	s.Log.Info("sessions revoked", "employee", id, "by", op.EmployeeID)
	writeUnlockJSON(w, map[string]any{"revoked": true})
}

package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sgao19/erp-go/pkg/grpcx"
)

// PermissionCache remembers what iam answered to "may this person do X" for
// a few seconds.
//
// Every guarded request asks iam once, and iam asks the database once. The
// 2026-10-04 load test showed this hop is where the queue forms when the
// machine gets busy: the answer itself takes a few milliseconds, but at about
// 270 requests a second iam's handlers waited ~170 ms for CPU, and every
// request in the company waited behind them. The answers barely change — a
// role is edited a few times a month — so asking again on every click is
// pure cost.
//
// How stale an answer may be is the bound the revocation snapshot already
// promises (revocationRefresh, ten seconds): a permission taken away stops
// working within that. Changes made through this gateway process — editing a
// role, assigning roles, deactivating somebody — do not wait even that long;
// the write clears the tenant's answers as it succeeds
// (forgetPermissionsOnChange). Another gateway process, if there ever is one,
// and changes made outside the gateway (preset roles reseeded at startup,
// migrations) fall back to the ten seconds.
//
// Only answers are kept, never errors: when iam is down a request fails as
// it always did, instead of being waved through on an old "yes".
type PermissionCache struct {
	ttl time.Duration
	max int
	now func() time.Time

	// Read by LogStats so the effect is visible after a deploy.
	hits, misses atomic.Int64

	mu      sync.Mutex
	entries map[permissionKey]permissionAnswer
	// Bumped by every forget. An answer fetched before a forget must not be
	// stored after it, or the change it raced would come back for a TTL.
	epochs map[int64]uint64
	global uint64
}

type permissionKey struct {
	tenant, employee int64
	code             string
}

type permissionAnswer struct {
	allowed bool
	expires time.Time
}

// permissionTicket is the state of the world a lookup saw; store only keeps
// an answer if nothing was forgotten in between.
type permissionTicket struct{ epoch, global uint64 }

const permissionCacheMax = 20_000

// PermissionCacheTTL is how long an answer is trusted: the same ten seconds
// a revoked session may outlive its revocation.
const PermissionCacheTTL = revocationRefresh

func NewPermissionCache(ttl time.Duration) *PermissionCache {
	return &PermissionCache{
		ttl: ttl, max: permissionCacheMax, now: time.Now,
		entries: map[permissionKey]permissionAnswer{}, epochs: map[int64]uint64{},
	}
}

// lookup returns a fresh answer if there is one; otherwise a ticket to store
// the answer about to be fetched.
func (c *PermissionCache) lookup(k permissionKey) (allowed, ok bool, ticket permissionTicket) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if a, found := c.entries[k]; found && c.now().Before(a.expires) {
		c.hits.Add(1)
		return a.allowed, true, permissionTicket{}
	}
	c.misses.Add(1)
	return false, false, permissionTicket{epoch: c.epochs[k.tenant], global: c.global}
}

func (c *PermissionCache) store(k permissionKey, allowed bool, ticket permissionTicket) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ticket.epoch != c.epochs[k.tenant] || ticket.global != c.global {
		return // a forget happened while this answer was in flight
	}
	if len(c.entries) >= c.max {
		c.dropExpiredLocked()
		if len(c.entries) >= c.max {
			// Every entry is fresh: more people than the bound, all active.
			// Starting over costs one iam call each, which is what every
			// request cost before this cache existed.
			c.entries = map[permissionKey]permissionAnswer{}
		}
	}
	c.entries[k] = permissionAnswer{allowed: allowed, expires: c.now().Add(c.ttl)}
}

func (c *PermissionCache) dropExpiredLocked() {
	now := c.now()
	for k, a := range c.entries {
		if !now.Before(a.expires) {
			delete(c.entries, k)
		}
	}
}

// forgetTenant drops every answer about one company's people.
func (c *PermissionCache) forgetTenant(tenant int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.epochs[tenant]++
	for k := range c.entries {
		if k.tenant == tenant {
			delete(c.entries, k)
		}
	}
}

// forgetAll drops everything, for changes that are not one company's.
func (c *PermissionCache) forgetAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.global++
	c.entries = map[permissionKey]permissionAnswer{}
}

// changesPermissions says whether a successful write to this path can change
// what somebody is allowed to do: who is active, who holds which role, what a
// role grants, whether a company is switched on.
//
// Fail-safe by prefix — a new route under these paths clears the cache until
// somebody decides it need not — minus the writes known to leave permissions
// alone. Those matter: avatar-urls is a read sent as POST that anyone with
// iam:employee:read may call, and every call would empty the tenant's cache.
func changesPermissions(path string) bool {
	if strings.HasPrefix(path, "/api/employees") {
		for _, suffix := range nonPermissionEmployeeWrites {
			if strings.HasSuffix(path, suffix) {
				return false
			}
		}
		return true
	}
	return strings.HasPrefix(path, "/api/roles") || strings.HasPrefix(path, "/api/platform/tenants")
}

// nonPermissionEmployeeWrites are /api/employees writes that change neither
// who is active nor who holds which role.
var nonPermissionEmployeeWrites = []string{
	"/avatar-urls", "/avatar", "/avatar/presign", "/invite", "/invite-batch",
	"/reset-link", "/password", "/company-mailbox", "/manager", "/revoke-sessions",
}

// forgetPermissionsOnChange clears cached answers the moment a write that can
// change them succeeds — before its response goes out, so the browser that
// made the change cannot outrun it.
func (s *Server) forgetPermissionsOnChange(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Perms == nil || !mutating(r.Method) || !changesPermissions(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		forget := func() {
			if strings.HasPrefix(r.URL.Path, "/api/platform/") {
				s.Perms.forgetAll()
			} else if op, ok := grpcx.OperatorFromContext(r.Context()); ok {
				s.Perms.forgetTenant(op.TenantID)
			}
		}
		tap := &successTap{ResponseWriter: w, onSuccess: forget}
		next.ServeHTTP(tap, r)
		if !tap.decided {
			// Returned without writing: net/http answers 200 for it.
			tap.decided = true
			forget()
		}
	})
}

// successTap runs onSuccess once, when the response turns out to be a
// success, just before the status line is written.
type successTap struct {
	http.ResponseWriter
	onSuccess func()
	decided   bool
}

func (t *successTap) WriteHeader(code int) {
	if !t.decided {
		t.decided = true
		if code < http.StatusBadRequest {
			t.onSuccess()
		}
	}
	t.ResponseWriter.WriteHeader(code)
}

func (t *successTap) Write(b []byte) (int, error) {
	if !t.decided {
		t.WriteHeader(http.StatusOK)
	}
	return t.ResponseWriter.Write(b)
}

// LogStats writes the hit rate every interval until ctx ends, so whether the
// cache is doing anything can be read from the logs after a deploy.
func (c *PermissionCache) LogStats(ctx context.Context, log *slog.Logger, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	var lastHits, lastMisses int64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hits, misses := c.hits.Load(), c.misses.Load()
			if dh, dm := hits-lastHits, misses-lastMisses; dh+dm > 0 {
				log.Info("permission cache", "event", "permission_cache", "hits", dh, "misses", dm,
					"hit_rate", float64(dh)/float64(dh+dm))
			}
			lastHits, lastMisses = hits, misses
		}
	}
}

package grpcx

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// AnswerCache keeps the answers to a few read-only RPCs for a short time, on
// the calling side.
//
// Built for iam's access questions — "may X do Y", "what may X do", "whose
// documents may X see". Every service asks them on almost every request, the
// answers change a few times a month, and the 2026-10-04 load test showed iam
// answering ~470 of them a second at 250 requests a second: the queries are
// sub-millisecond, but under load each one waited for CPU and every request
// in the company waited behind them.
//
// An answer is trusted for the TTL — ten seconds, the bound the gateway's
// session revocation already promises. The gateway also forgets a company's
// answers the moment a role or employee change it relays succeeds; the other
// services have no such hook and rely on the TTL alone.
//
// Only successful answers are kept: when iam fails, the call fails as it
// always did, instead of being answered from an old "yes".
type AnswerCache struct {
	ttl     time.Duration
	max     int
	methods map[string]bool
	now     func() time.Time

	hits, misses atomic.Int64

	mu      sync.Mutex
	entries map[string]cachedAnswer
	// Bumped by every forget. An answer fetched before a forget must not be
	// stored after it, or the change it raced would come back for a TTL.
	epochs map[int64]uint64
	global uint64
}

type cachedAnswer struct {
	tenant  int64
	reply   proto.Message
	expires time.Time
}

type answerTicket struct{ epoch, global uint64 }

// AnswerTTL is how long an access answer is trusted.
const AnswerTTL = 10 * time.Second

const answerCacheMax = 20_000

// The iam questions worth remembering. Writes, and questions whose answer is
// a moving list of people (ListRoleMembers for approval routing), are not.
var accessQuestions = []string{
	"/erp.iam.v1.AccessService/CheckPermission",
	"/erp.iam.v1.AccessService/ListEmployeePermissions",
	"/erp.iam.v1.AccessService/VisibleEmployees",
}

// NewAccessAnswerCache remembers iam's access answers for AnswerTTL.
func NewAccessAnswerCache() *AnswerCache {
	return NewAnswerCache(AnswerTTL, accessQuestions...)
}

func NewAnswerCache(ttl time.Duration, methods ...string) *AnswerCache {
	c := &AnswerCache{
		ttl: ttl, max: answerCacheMax, methods: map[string]bool{}, now: time.Now,
		entries: map[string]cachedAnswer{}, epochs: map[int64]uint64{},
	}
	for _, m := range methods {
		c.methods[m] = true
	}
	return c
}

// Interceptor answers the cached methods from memory when it can.
//
// The key is the method, the caller's company and identity and the whole
// request. The
// company comes from the operator riding the context — the same one
// UnaryClientPropagator signs into the call, and the one iam scopes the
// answer by — so two companies never share an answer.
func (c *AnswerCache) Interceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any,
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		in, inOK := req.(proto.Message)
		out, outOK := reply.(proto.Message)
		if !c.methods[method] || !inOK || !outOK {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		body, err := proto.MarshalOptions{Deterministic: true}.Marshal(in)
		if err != nil {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		op, _ := OperatorFromContext(ctx)
		// Who is asking is in the key even though iam does not look at it
		// today: if it ever answers differently depending on the asker, the
		// cache must not hand one person's answer to another.
		key := method + "\x00" + strconv.FormatInt(op.TenantID, 10) + "\x00" +
			strconv.FormatInt(op.EmployeeID, 10) + "\x00" + string(body)
		cached, ticket := c.lookup(key, op.TenantID)
		if cached != nil {
			proto.Reset(out)
			proto.Merge(out, cached)
			return nil
		}
		if err := invoker(ctx, method, req, reply, cc, opts...); err != nil {
			return err
		}
		c.store(key, op.TenantID, proto.Clone(out), ticket)
		return nil
	}
}

func (c *AnswerCache) lookup(key string, tenant int64) (proto.Message, answerTicket) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if a, ok := c.entries[key]; ok && c.now().Before(a.expires) {
		c.hits.Add(1)
		return a.reply, answerTicket{}
	}
	c.misses.Add(1)
	return nil, answerTicket{epoch: c.epochs[tenant], global: c.global}
}

func (c *AnswerCache) store(key string, tenant int64, reply proto.Message, t answerTicket) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t.epoch != c.epochs[tenant] || t.global != c.global {
		return // a forget happened while this answer was in flight
	}
	if len(c.entries) >= c.max {
		now := c.now()
		for k, a := range c.entries {
			if !now.Before(a.expires) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= c.max {
			// Every entry is fresh. Starting over costs one iam call each,
			// which is what every call cost before this cache existed.
			c.entries = map[string]cachedAnswer{}
		}
	}
	c.entries[key] = cachedAnswer{tenant: tenant, reply: reply, expires: c.now().Add(c.ttl)}
}

// ForgetTenant drops every answer given to one company.
func (c *AnswerCache) ForgetTenant(tenant int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.epochs[tenant]++
	for k, a := range c.entries {
		if a.tenant == tenant {
			delete(c.entries, k)
		}
	}
}

// ForgetAll drops everything, for changes that are not one company's.
func (c *AnswerCache) ForgetAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.global++
	c.entries = map[string]cachedAnswer{}
}

// LogStats writes the hit rate every interval until ctx ends, so whether the
// cache does anything can be read from the logs after a deploy.
func (c *AnswerCache) LogStats(ctx context.Context, log *slog.Logger, every time.Duration) {
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
				log.Info("access answer cache", "event", "access_answer_cache", "hits", dh, "misses", dm,
					"hit_rate", float64(dh)/float64(dh+dm))
			}
			lastHits, lastMisses = hits, misses
		}
	}
}

// CacheAccessAnswers is the dial option every service puts on its iam
// connection, with the hit rate logged every ten minutes. The cache comes
// back too, for the one caller that can also forget (the gateway).
func CacheAccessAnswers(ctx context.Context, log *slog.Logger) (grpc.DialOption, *AnswerCache) {
	c := NewAccessAnswerCache()
	go c.LogStats(ctx, log, 10*time.Minute)
	return grpc.WithChainUnaryInterceptor(c.Interceptor()), c
}

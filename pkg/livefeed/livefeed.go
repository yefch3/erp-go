// Package livefeed carries short-lived "something you are looking at just
// changed" pings from a service to whoever has the page open.
//
// It is deliberately NOT the event bus. Kafka carries the business record:
// durable, ordered, replayable, and consumed by other services. These pings
// are none of those things — they are hints to a browser, and a lost one only
// means the user sees stale data until the next refresh, which is exactly
// where the system stood before. Redis pub/sub matches that: fire and forget,
// no storage, and every gateway replica gets its own copy without having to
// invent a unique consumer group.
package livefeed

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// Event is what reaches the browser. Deliberately thin: it says what kind of
// thing changed, not what it now says. The page re-fetches through the normal
// API, so permissions are re-checked and there is one code path for loading
// data rather than two that can disagree.
type Event struct {
	Type string `json:"type"`
	// Optional hint about which object moved, e.g. "CONTRACT:4", so a page
	// showing one document can ignore pings about others.
	Subject string    `json:"subject,omitempty"`
	At      time.Time `json:"at"`
}

// Event types. Keep the list short; a page that has to switch on twenty of
// these has the wrong design.
const (
	// TodoChanged: this employee's approval queue is not what they last saw.
	TodoChanged = "todo.changed"
	// DocChanged: a document this employee submitted has moved on.
	DocChanged = "doc.changed"
	// RequirementChanged: the purchase requirement list moved — stock covered
	// a shortage, an order was raised, goods arrived. Addressed to the whole
	// tenant rather than a person: a requirement belongs to the purchasing
	// function, and there is no "owner" to send it to.
	RequirementChanged = "requirement.changed"
)

func channel(tenantID, employeeID int64) string {
	return fmt.Sprintf("erp.live.t%d.e%d", tenantID, employeeID)
}

// broadcastChannel carries changes to shared functional data — purchasing,
// stock, shipping — that no single employee owns.
//
// Everyone hears it, which is fine because the payload says only "go and
// re-read": the re-read goes through the normal permission-checked API, so
// somebody without the permission learns nothing they could not already ask
// for. Sending the change itself over this channel would not be safe.
func broadcastChannel(tenantID int64) string {
	return fmt.Sprintf("erp.live.t%d.all", tenantID)
}

// Publisher is held by services that cause changes.
type Publisher struct {
	rdb *redis.Client
	log *slog.Logger
}

func NewPublisher(addr string, log *slog.Logger) *Publisher {
	return &Publisher{rdb: redis.NewClient(&redis.Options{Addr: addr}), log: log}
}

func (p *Publisher) Close() error { return p.rdb.Close() }

// ToEmployees delivers one event to several people at once, best effort.
//
// It never returns an error and must be called AFTER the transaction that
// caused the change has committed. A ping sent from inside a transaction that
// then rolls back would tell the browser to go and read something that never
// happened; a ping that fails to send costs nothing at all.
func (p *Publisher) ToEmployees(ctx context.Context, tenantID int64, employeeIDs []int64, e Event) {
	if len(employeeIDs) == 0 {
		return
	}
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	body, err := json.Marshal(e)
	if err != nil {
		p.log.Warn("livefeed: marshal failed", "err", err)
		return
	}
	seen := make(map[int64]bool, len(employeeIDs))
	for _, id := range employeeIDs {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		if err := p.rdb.Publish(ctx, channel(tenantID, id), body).Err(); err != nil {
			// Losing a hint is not worth failing a business operation over.
			p.log.Warn("livefeed: publish failed",
				"tenant", tenantID, "employee", id, "type", e.Type, "err", err)
		}
	}
}

// ToTenant delivers one event to everybody in a tenant who has a page open.
// Same rules as ToEmployees: after the commit, never inside it, and a failure
// is logged rather than propagated.
func (p *Publisher) ToTenant(ctx context.Context, tenantID int64, e Event) {
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	body, err := json.Marshal(e)
	if err != nil {
		p.log.Warn("livefeed: marshal failed", "err", err)
		return
	}
	if err := p.rdb.Publish(ctx, broadcastChannel(tenantID), body).Err(); err != nil {
		p.log.Warn("livefeed: broadcast failed",
			"tenant", tenantID, "type", e.Type, "err", err)
	}
}

// Subscriber is held by the gateway, which is the only thing browsers talk to.
type Subscriber struct {
	rdb *redis.Client
}

func NewSubscriber(addr string) *Subscriber {
	return &Subscriber{rdb: redis.NewClient(&redis.Options{Addr: addr})}
}

func (s *Subscriber) Close() error { return s.rdb.Close() }

// Ping reports whether Redis is reachable, so the gateway can fail loudly at
// startup instead of silently serving a stream that never delivers anything.
func (s *Subscriber) Ping(ctx context.Context) error { return s.rdb.Ping(ctx).Err() }

// Listen returns a channel of events addressed to one employee. The returned
// channel closes when ctx is cancelled, which is what happens when the browser
// disconnects.
func (s *Subscriber) Listen(ctx context.Context, tenantID, employeeID int64) <-chan Event {
	// Both channels on one subscription: what is addressed to this person, and
	// what changed in the shared functional data everybody works from.
	sub := s.rdb.Subscribe(ctx, channel(tenantID, employeeID), broadcastChannel(tenantID))
	out := make(chan Event)

	// The context passed to Subscribe only covers the initial call: go-redis
	// keeps the subscription open until it is explicitly closed, and the
	// message channel never closes on its own. Without this watcher the
	// receive loop below would block forever on a silent channel, leaking a
	// goroutine and a Redis subscription for every page anyone ever opened.
	go func() {
		<-ctx.Done()
		_ = sub.Close()
	}()

	go func() {
		defer close(out)
		for msg := range sub.Channel() {
			var e Event
			if err := json.Unmarshal([]byte(msg.Payload), &e); err != nil {
				continue // a malformed hint is simply dropped
			}
			select {
			case out <- e:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

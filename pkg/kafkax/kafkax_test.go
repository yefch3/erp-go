package kafkax

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeReader replays a script of fetch outcomes, then blocks until the
// context ends. It stands in for a broker that drops out mid-session.
type fakeReader struct {
	mu       sync.Mutex
	script   []error // one entry per FetchMessage call; nil means "deliver msg"
	calls    int
	msg      kafka.Message
	commits  int
	closed   bool
	exceeded chan struct{} // closed once the script is used up
}

func newFakeReader(script ...error) *fakeReader {
	v, _ := json.Marshal(Envelope{EventID: 1, AggregateType: "contract", EventType: "x"})
	return &fakeReader{
		script:   script,
		msg:      kafka.Message{Topic: "t", Value: v},
		exceeded: make(chan struct{}),
	}
}

func (f *fakeReader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	f.mu.Lock()
	n := f.calls
	f.calls++
	f.mu.Unlock()

	if n >= len(f.script) {
		if n == len(f.script) {
			close(f.exceeded)
		}
		<-ctx.Done() // script exhausted: park so Run keeps running
		return kafka.Message{}, ctx.Err()
	}
	if err := f.script[n]; err != nil {
		return kafka.Message{}, err
	}
	return f.msg, nil
}

func (f *fakeReader) CommitMessages(context.Context, ...kafka.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commits++
	return nil
}

func (f *fakeReader) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeReader) counts() (calls, commits int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.commits
}

type okDeduper struct{}

func (okDeduper) MarkProcessed(context.Context, string) (bool, error) { return true, nil }

func TestNextRetryStartsAtFloorDoublesAndStopsAtCeiling(t *testing.T) {
	if got := nextRetry(0); got != fetchRetryMin {
		t.Fatalf("first retry = %v, want floor %v", got, fetchRetryMin)
	}
	if got := nextRetry(time.Second); got != 2*time.Second {
		t.Fatalf("retry after 1s = %v, want 2s", got)
	}
	if got := nextRetry(fetchRetryMax); got != fetchRetryMax {
		t.Fatalf("retry at ceiling = %v, want it to stay at %v", got, fetchRetryMax)
	}
	if got := nextRetry(fetchRetryMax - time.Second); got != fetchRetryMax {
		t.Fatalf("retry overshooting ceiling = %v, want clamped to %v", got, fetchRetryMax)
	}
}

// The regression: a broker that goes away mid-session used to end Run for
// good. The goroutine in each service's main exited, the service stayed up
// and healthy, and it silently consumed nothing until someone restarted it.
// Restarting Kafka was enough to trigger it - a routine event on a cloud host.
func TestFetchErrorIsRetriedRatherThanEndingTheConsumer(t *testing.T) {
	dial := errors.New("failed to dial: connect: connection refused")
	f := newFakeReader(dial, dial, dial)

	c := &Consumer{r: f, topic: "t", group: "g",
		dedupe: okDeduper{}, handler: func(context.Context, Envelope) error { return nil },
		log: discardLogger()}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	// Run must work through all three failures and ask for a fourth message.
	select {
	case <-f.exceeded:
	case err := <-done:
		t.Fatalf("Run returned %v after a fetch error; the broker coming back "+
			"must resume consumption, not require a service restart", err)
	case <-time.After(10 * time.Second):
		calls, _ := f.counts()
		t.Fatalf("Run stalled after %d fetch attempts", calls)
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v on shutdown, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run ignored context cancellation")
	}
	if !f.closed {
		t.Error("Run did not close the reader on the way out")
	}
}

// Recovery is the point of the retry: messages arriving after the outage
// must be processed and committed like any other.
func TestConsumerProcessesMessagesAfterAnOutage(t *testing.T) {
	f := newFakeReader(errors.New("connection refused"), nil, nil)

	var handled int
	var mu sync.Mutex
	c := &Consumer{r: f, topic: "t", group: "g", dedupe: okDeduper{},
		handler: func(context.Context, Envelope) error {
			mu.Lock()
			handled++
			mu.Unlock()
			return nil
		},
		log: discardLogger()}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = c.Run(ctx) }()

	select {
	case <-f.exceeded:
	case <-time.After(10 * time.Second):
		t.Fatal("consumer never worked through the script")
	}
	cancel()

	mu.Lock()
	got := handled
	mu.Unlock()
	if got != 2 {
		t.Errorf("handled %d messages after the outage, want 2", got)
	}
	if _, commits := f.counts(); commits != 2 {
		t.Errorf("committed %d offsets, want 2", commits)
	}
}

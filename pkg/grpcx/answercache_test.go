package grpcx

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const askedMethod = "/erp.iam.v1.AccessService/CheckPermission"

// fakeIAM stands in for the network: it answers "<request>:<call number>" so
// a test can tell a fresh answer from a remembered one.
type fakeIAM struct {
	calls int
	fail  error
}

func (f *fakeIAM) invoke(_ context.Context, _ string, req, reply any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
	f.calls++
	if f.fail != nil {
		return f.fail
	}
	reply.(*wrapperspb.StringValue).Value = req.(*wrapperspb.StringValue).GetValue() + ":" + string(rune('0'+f.calls))
	return nil
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestCache() (*AnswerCache, *clock) {
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	cache := NewAnswerCache(AnswerTTL, askedMethod)
	cache.now = c.now
	return cache, c
}

func ask(t *testing.T, cache *AnswerCache, iam *fakeIAM, tenant int64, method, question string) string {
	t.Helper()
	ctx := WithOperator(context.Background(), Operator{TenantID: tenant, EmployeeID: 9})
	reply := &wrapperspb.StringValue{}
	if err := cache.Interceptor()(ctx, method, wrapperspb.String(question), reply, nil, iam.invoke); err != nil {
		t.Fatalf("ask: %v", err)
	}
	return reply.GetValue()
}

func TestAnswersAreReusedWithinTheTTL(t *testing.T) {
	cache, clk := newTestCache()
	iam := &fakeIAM{}
	first := ask(t, cache, iam, 4, askedMethod, "sales:inquiry:read")
	for i := 0; i < 3; i++ {
		if got := ask(t, cache, iam, 4, askedMethod, "sales:inquiry:read"); got != first {
			t.Fatalf("remembered answer changed: %q vs %q", got, first)
		}
	}
	if iam.calls != 1 {
		t.Fatalf("iam asked %d times", iam.calls)
	}
	clk.t = clk.t.Add(AnswerTTL)
	if got := ask(t, cache, iam, 4, askedMethod, "sales:inquiry:read"); got == first || iam.calls != 2 {
		t.Fatalf("an expired answer was reused: %q after %d calls", got, iam.calls)
	}
}

func TestAnswersAreKeptApartByQuestionAndCompany(t *testing.T) {
	cache, _ := newTestCache()
	iam := &fakeIAM{}
	a := ask(t, cache, iam, 4, askedMethod, "x")
	b := ask(t, cache, iam, 4, askedMethod, "y")
	c := ask(t, cache, iam, 5, askedMethod, "x")
	if a == b || a == c || iam.calls != 3 {
		t.Fatalf("answers shared across questions or companies: %q %q %q (%d calls)", a, b, c, iam.calls)
	}
}

func TestOtherMethodsAndErrorsPassThrough(t *testing.T) {
	cache, _ := newTestCache()
	iam := &fakeIAM{}
	ask(t, cache, iam, 4, "/erp.iam.v1.AccessService/ListRoleMembers", "r")
	ask(t, cache, iam, 4, "/erp.iam.v1.AccessService/ListRoleMembers", "r")
	if iam.calls != 2 {
		t.Fatalf("an uncached method was cached: %d calls", iam.calls)
	}
	iam.fail = errors.New("iam down")
	ctx := WithOperator(context.Background(), Operator{TenantID: 4})
	if err := cache.Interceptor()(ctx, askedMethod, wrapperspb.String("z"), &wrapperspb.StringValue{}, nil, iam.invoke); err == nil {
		t.Fatal("an iam failure was swallowed")
	}
	iam.fail = nil
	if got := ask(t, cache, iam, 4, askedMethod, "z"); got == "" {
		t.Fatal("a failure was remembered")
	}
}

func TestCallersCannotChangeARememberedAnswer(t *testing.T) {
	cache, _ := newTestCache()
	iam := &fakeIAM{}
	ctx := WithOperator(context.Background(), Operator{TenantID: 4, EmployeeID: 9})
	reply := &wrapperspb.StringValue{}
	_ = cache.Interceptor()(ctx, askedMethod, wrapperspb.String("q"), reply, nil, iam.invoke)
	want := reply.Value
	reply.Value = "tampered"
	if got := ask(t, cache, iam, 4, askedMethod, "q"); got != want {
		t.Fatalf("caller's edit leaked into the cache: %q", got)
	}
}

func TestForgettingWinsOverAnAnswerInFlight(t *testing.T) {
	cache, _ := newTestCache()
	_, ticket := cache.lookup("k", 4)
	cache.ForgetTenant(4)
	cache.store("k", 4, wrapperspb.String("old"), ticket)
	if got, _ := cache.lookup("k", 4); got != nil {
		t.Fatal("an answer from before the change was stored after it")
	}
	_, ticket = cache.lookup("k", 4)
	cache.ForgetAll()
	cache.store("k", 4, wrapperspb.String("old"), ticket)
	if got, _ := cache.lookup("k", 4); got != nil {
		t.Fatal("an answer from before a global change was stored after it")
	}
	_, ticket = cache.lookup("k", 4)
	cache.ForgetTenant(5)
	cache.store("k", 4, wrapperspb.String("kept"), ticket)
	if got, _ := cache.lookup("k", 4); got == nil {
		t.Fatal("another company's change dropped this answer")
	}
}

func TestForgetTenantOnlyDropsThatCompany(t *testing.T) {
	cache, _ := newTestCache()
	iam := &fakeIAM{}
	ask(t, cache, iam, 4, askedMethod, "x")
	ask(t, cache, iam, 5, askedMethod, "x")
	cache.ForgetTenant(4)
	ask(t, cache, iam, 4, askedMethod, "x")
	ask(t, cache, iam, 5, askedMethod, "x")
	if iam.calls != 3 {
		t.Fatalf("forget reached the wrong company: %d calls", iam.calls)
	}
}

func TestAnswerCacheStaysBounded(t *testing.T) {
	cache, _ := newTestCache()
	cache.max = 3
	iam := &fakeIAM{}
	for i := 0; i < 10; i++ {
		ask(t, cache, iam, 4, askedMethod, string(rune('a'+i)))
		if len(cache.entries) > cache.max {
			t.Fatalf("cache grew to %d", len(cache.entries))
		}
	}
}

func TestAnswersAreKeptApartByWhoAsks(t *testing.T) {
	cache, _ := newTestCache()
	iam := &fakeIAM{}
	for _, employee := range []int64{9, 10} {
		ctx := WithOperator(context.Background(), Operator{TenantID: 4, EmployeeID: employee})
		_ = cache.Interceptor()(ctx, askedMethod, wrapperspb.String("q"), &wrapperspb.StringValue{}, nil, iam.invoke)
	}
	if iam.calls != 2 {
		t.Fatalf("one person's answer was handed to another: %d calls", iam.calls)
	}
}

// Many requests asking while an administrator's change forgets: run with
// -race, which is the point of this test.
func TestConcurrentAskingAndForgetting(t *testing.T) {
	cache := NewAnswerCache(AnswerTTL, askedMethod)
	var mu sync.Mutex
	invoke := func(_ context.Context, _ string, req, reply any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
		mu.Lock()
		defer mu.Unlock()
		reply.(*wrapperspb.StringValue).Value = req.(*wrapperspb.StringValue).GetValue()
		return nil
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			ctx := WithOperator(context.Background(), Operator{TenantID: int64(g % 2), EmployeeID: int64(g)})
			for i := 0; i < 200; i++ {
				q := string(rune('a' + i%5))
				reply := &wrapperspb.StringValue{}
				if err := cache.Interceptor()(ctx, askedMethod, wrapperspb.String(q), reply, nil, invoke); err != nil || reply.Value != q {
					t.Errorf("wrong answer %q for %q (%v)", reply.Value, q, err)
					return
				}
				if i%50 == 0 {
					cache.ForgetTenant(int64(g % 2))
				}
			}
		}(g)
	}
	wg.Wait()
}

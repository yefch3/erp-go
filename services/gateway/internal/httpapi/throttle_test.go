package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

// fakeCounter is the storage the throttle would otherwise get from Redis. The
// decisions worth testing here are about counts and windows; standing up a
// Redis to assert that eleven is more than ten would be testing Redis.
type fakeCounter struct {
	n      map[string]int64
	ttl    map[string]time.Duration
	broken error
	// Every key Bump has been called with, so a test can prove which identity
	// a budget was spent against rather than only that one was.
	bumped []string
}

func newFakeCounter() *fakeCounter {
	return &fakeCounter{n: map[string]int64{}, ttl: map[string]time.Duration{}}
}

func (f *fakeCounter) Bump(_ context.Context, key string, window time.Duration) (int64, time.Duration, error) {
	if f.broken != nil {
		return 0, 0, f.broken
	}
	f.bumped = append(f.bumped, key)
	f.n[key]++
	if _, open := f.ttl[key]; !open {
		f.ttl[key] = window
	}
	return f.n[key], f.ttl[key], nil
}

func (f *fakeCounter) Peek(_ context.Context, key string) (int64, time.Duration, error) {
	if f.broken != nil {
		return 0, 0, f.broken
	}
	return f.n[key], f.ttl[key], nil
}

func (f *fakeCounter) Clear(_ context.Context, key string) error {
	if f.broken != nil {
		return f.broken
	}
	delete(f.n, key)
	delete(f.ttl, key)
	return nil
}

func newTestThrottle() (*FailureThrottle, *fakeCounter) {
	c := newFakeCounter()
	return &FailureThrottle{c: c}, c
}

func TestTheBudgetIsSpentOnlyOnTheLastFailure(t *testing.T) {
	th, _ := newTestThrottle()
	ctx := context.Background()

	for i := 1; i < loginMaxFailures; i++ {
		if _, spent := th.Failed(ctx, throttleLogin, "kratos"); spent {
			t.Fatalf("locked out after %d failures, budget is %d", i, loginMaxFailures)
		}
		if _, blocked := th.Blocked(ctx, throttleLogin, "kratos"); blocked {
			t.Fatalf("blocked after %d failures, budget is %d", i, loginMaxFailures)
		}
	}
	wait, spent := th.Failed(ctx, throttleLogin, "kratos")
	if !spent {
		t.Fatalf("failure %d did not spend the budget", loginMaxFailures)
	}
	if wait != loginWindow {
		t.Fatalf("retry-after = %v, want the window %v", wait, loginWindow)
	}
	if _, blocked := th.Blocked(ctx, throttleLogin, "kratos"); !blocked {
		t.Fatal("a later attempt was allowed through after the budget was spent")
	}
}

func TestSucceedingForgetsEarlierFailures(t *testing.T) {
	th, _ := newTestThrottle()
	ctx := context.Background()

	for i := 0; i < loginMaxFailures-1; i++ {
		th.Failed(ctx, throttleLogin, "kratos")
	}
	th.Passed(ctx, throttleLogin, "kratos")

	// The count is of consecutive failures: somebody who mistypes nine times
	// and then logs in is not one mistake from a lockout.
	for i := 1; i < loginMaxFailures; i++ {
		if _, spent := th.Failed(ctx, throttleLogin, "kratos"); spent {
			t.Fatalf("locked out after %d failures following a success", i)
		}
	}
}

func TestOneAccountsFailuresDoNotLockAnother(t *testing.T) {
	th, _ := newTestThrottle()
	ctx := context.Background()

	for i := 0; i < loginMaxFailures*2; i++ {
		th.Failed(ctx, throttleLogin, "kratos")
	}
	if _, blocked := th.Blocked(ctx, throttleLogin, "someone-else"); blocked {
		t.Fatal("a colleague was locked out by somebody else's failures")
	}
}

// The two routes have different budgets because they cost different things,
// which is worth nothing if they share a counter.
func TestLoginAndMailboxVerifyDoNotSpendEachOthersBudget(t *testing.T) {
	th, _ := newTestThrottle()
	ctx := context.Background()

	for i := 0; i < mailVerifyMaxFailures; i++ {
		th.Failed(ctx, throttleMailVerify, "t1.e1")
	}
	if _, blocked := th.Blocked(ctx, throttleMailVerify, "t1.e1"); !blocked {
		t.Fatal("mailbox verification was not blocked at its own limit")
	}
	if _, blocked := th.Blocked(ctx, throttleLogin, "t1.e1"); blocked {
		t.Fatal("mailbox failures spent the login budget for the same name")
	}
}

func TestMailExcelHasAFixedRequestBudget(t *testing.T) {
	th, _ := newTestThrottle()
	ctx := context.Background()
	for i := 0; i < mailExcelMaxRequests; i++ {
		if _, limited := th.Limited(ctx, throttleMailExcel, "t1.e1"); limited {
			t.Fatalf("request %d was limited inside the budget", i+1)
		}
	}
	wait, limited := th.Limited(ctx, throttleMailExcel, "t1.e1")
	if !limited {
		t.Fatal("first request beyond the Excel budget was allowed")
	}
	if wait != mailExcelWindow {
		t.Fatalf("retry-after = %v, want %v", wait, mailExcelWindow)
	}
	if _, limited := th.Limited(ctx, throttleMailExcel, "t1.e2"); limited {
		t.Fatal("one employee spent another employee's Excel budget")
	}
}

func TestMailboxVerifyIsStricterThanLogin(t *testing.T) {
	// Not a style preference: a verification attempt is a real login to Gmail
	// or 263 from our IP, so it has to cost more than a guess at our own
	// password does.
	//
	// Compared as a rate rather than as a count, because the two counters stopped
	// being the same shape when login moved to a sixty-second window — twenty
	// failures a minute and five failures a quarter of an hour are not two
	// numbers that can be put beside each other.
	loginPerHour := float64(loginMaxFailures) / loginWindow.Hours()
	verifyPerHour := float64(mailVerifyMaxFailures) / mailVerifyWindow.Hours()
	if verifyPerHour >= loginPerHour {
		t.Fatalf("mailbox verify allows %.0f/hour, login allows %.0f/hour — the one that "+
			"spends our server's reputation with the mail host must be tighter",
			verifyPerHour, loginPerHour)
	}
}

// The login limit is keyed on where the request came from, and the only value
// a caller cannot choose is the socket it arrived on. X-Forwarded-For is a
// header anybody can send, so honouring it by default would turn a per-source
// limit into no limit at all: a sprayer puts a new fake address on every
// request and never spends a budget.
func TestAForgedForwardedHeaderIsIgnoredByDefault(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	r.RemoteAddr = "203.0.113.9:51234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := clientAddr(r, false); got != "203.0.113.9" {
		t.Fatalf("clientAddr trusted a header it was not told to trust: %q", got)
	}
}

func TestTheForwardedHeaderIsUsedOnlyWhenADeploymentSaysSo(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	r.RemoteAddr = "10.0.0.5:33001"
	r.Header.Set("X-Forwarded-For", "198.51.100.7, 10.0.0.5")
	if got := clientAddr(r, true); got != "198.51.100.7" {
		t.Fatalf("clientAddr took %q, want the leftmost entry", got)
	}
}

// The port is different on every connection. Keying on it would give each
// attempt its own bucket, which is the same as not counting.
func TestThePortIsNotPartOfTheIdentity(t *testing.T) {
	first := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	first.RemoteAddr = "203.0.113.9:51234"
	second := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	second.RemoteAddr = "203.0.113.9:60999"
	if clientAddr(first, false) != clientAddr(second, false) {
		t.Fatal("two connections from one address counted separately")
	}
}

// The window is short on purpose, and the reason is worth a test rather than
// only a comment: it is what makes metering by source safe when a whole office
// shares one egress address. Lengthen it and twenty mistyped passwords in an
// afternoon shut out everybody sitting in that room.
func TestTheSourceWindowStaysShortEnoughToShare(t *testing.T) {
	if loginWindow > 2*time.Minute {
		t.Fatalf("the per-source window is %v; a shared office address cannot afford that", loginWindow)
	}
	if loginMaxFailures < 20 {
		t.Fatalf("the per-source budget is %d, which a shared office address would hit by accident", loginMaxFailures)
	}
}

// Sabotage: if the counter's storage goes down, the throttle must not lock the
// whole company out of the ERP. It fails open, unlike the mailbox unlock store,
// and this test is what keeps somebody from "fixing" that inconsistency.
func TestAStorageOutageAllowsAttemptsRatherThanBlockingEveryone(t *testing.T) {
	th, c := newTestThrottle()
	ctx := context.Background()
	c.broken = errors.New("redis is down")

	if _, blocked := th.Blocked(ctx, throttleLogin, "kratos"); blocked {
		t.Fatal("blocked a login because the counter was unreachable")
	}
	if _, spent := th.Failed(ctx, throttleLogin, "kratos"); spent {
		t.Fatal("reported a spent budget from a failed write")
	}
	th.Passed(ctx, throttleLogin, "kratos") // must not panic
}

// Sabotage: a throttle that was never wired up must not silently pretend to
// meter anything, and must not panic on the way past.
func TestAnAbsentThrottleAllowsEverything(t *testing.T) {
	var th *FailureThrottle
	ctx := context.Background()

	if _, blocked := th.Blocked(ctx, throttleLogin, "kratos"); blocked {
		t.Fatal("a nil throttle blocked an attempt")
	}
	if _, spent := th.Failed(ctx, throttleLogin, "kratos"); spent {
		t.Fatal("a nil throttle reported a spent budget")
	}
	th.Passed(ctx, throttleLogin, "kratos")
}

// An empty username is every caller who posted no username at all. Metering
// them together would let one malformed client lock out the next.
func TestAnEmptyIdentityIsNotMetered(t *testing.T) {
	th, c := newTestThrottle()
	ctx := context.Background()

	for i := 0; i < loginMaxFailures*3; i++ {
		th.Failed(ctx, throttleLogin, "")
	}
	if _, blocked := th.Blocked(ctx, throttleLogin, ""); blocked {
		t.Fatal("callers with no username were metered as one identity")
	}
	if len(c.bumped) != 0 {
		t.Fatalf("wrote %d counters for an empty identity", len(c.bumped))
	}
}

func TestTheSameAccountIsOneIdentityHoweverItIsTyped(t *testing.T) {
	// Otherwise the budget is per-spelling, and " Kratos" is a fresh ten
	// guesses.
	for _, id := range []string{"Kratos", " kratos", "KRATOS ", "kratos"} {
		if got, want := throttleKey(throttleLogin, id), throttleKey(throttleLogin, "kratos"); got != want {
			t.Fatalf("%q keyed as %s, want %s", id, got, want)
		}
	}
}

func TestTheStoredKeyDoesNotSpellOutTheAccount(t *testing.T) {
	// The key is what lands in a store meant for throwaway state; a Redis full
	// of plaintext usernames is a list of who has an account here.
	if key := throttleKey(throttleLogin, "kratos"); contains(key, "kratos") {
		t.Fatalf("key %q carries the username", key)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// An outage must not be charged to the people it happens to. Without this, iam
// going down for five minutes ends with everybody in the company locked out
// for fifteen more — a recovery worse than the fault.
func TestOnlyARejectedCredentialCountsAgainstTheBudget(t *testing.T) {
	rejected := []codes.Code{codes.Unauthenticated, codes.PermissionDenied}
	ours := []codes.Code{
		codes.Unavailable, codes.DeadlineExceeded, codes.Internal,
		codes.ResourceExhausted, codes.Unknown, codes.Canceled,
	}
	for _, c := range rejected {
		if !isRejectedCredential(status.Error(c, "")) {
			t.Errorf("%v should count: it is the caller getting it wrong", c)
		}
	}
	for _, c := range ours {
		if isRejectedCredential(status.Error(c, "")) {
			t.Errorf("%v should not count: it is us being broken", c)
		}
	}
	if isRejectedCredential(nil) {
		t.Error("a successful call counted as a failure")
	}
}

// 绑定入口放宽了：地址现在是请求体里的字段。这条测试是**替换**上一条，
// 不是删掉它。
//
// 上一条叫 TestTheVerifyRequestCarriesNoAddress，断言 verifyMailbox 的函数体
// 里没有 json:"email"、有 Email: op.Email。它防的是「以 alice@thecompany.com
// 登录，却绑一个私人信箱」，而那条规矩本身是修过一次 bug 之后立的。
//
// 它退役是因为业务口径变了：一个人可以绑多个信箱，而且**不必是公司域名的**
// ——ERP 账号是 263 的人，邮箱这边可以只绑 Gmail。老约束和这个需求直接冲突，
// 不是"忘了"或"绕过"。
//
// 换上的四条写在这里。它们和老的那条一样是源码文本断言，理由也一样：这几件
// 事没有一个运行时的地方能一眼看出来，而删掉其中任何一条都不会让别的测试变红。
func TestBindingAnAddressStillHasGuards(t *testing.T) {
	src, err := os.ReadFile("mailunlock.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	start := strings.Index(body, "func (s *Server) verifyMailbox")
	if start < 0 {
		t.Fatal("verifyMailbox is gone; this test needs rewriting")
	}
	end := strings.Index(body[start:], "\nfunc ")
	fn := body[start : start+end]

	// 一、绑给谁只能来自登录令牌。请求体里出现「绑给谁」这一项，等于任何
	// 登录了的人都能把一个信箱挂到别人名下。
	if strings.Contains(fn, `json:"employeeId"`) || strings.Contains(fn, `json:"employee_id"`) {
		t.Error("请求体里出现了「绑给谁」——归属必须只来自登录令牌，" +
			"否则任何人都能把信箱挂到别人名下")
	}
	if !strings.Contains(fn, "grpcx.OperatorFromContext") {
		t.Error("不再从登录令牌取操作人了")
	}

	// 二、**两份预算都得在**：按人的那份和按人+地址的那份。
	//
	// 只留按人+地址那一份是错的（第一版就是这么写的）：地址成了可变字段
	// 之后，它等于把「一个人五次」稀释成「一个人每个地址五次」，换一串
	// 地址就换一份预算，比原来更弱。这个预算保护的是我们服务器的 IP 在
	// 邮件服务商那里的信誉，而那个资源是按人花的。
	if !strings.Contains(fn, `perPerson := fmt.Sprintf("t%d.e%d"`) {
		t.Error("按人那一份预算没了——换一串地址就能换一份新预算")
	}
	if !strings.Contains(fn, `perTarget := fmt.Sprintf("t%d.e%d.%s"`) {
		t.Error("按目标地址那一份预算没了——对着同一个信箱可以一直试")
	}
	if !strings.Contains(fn, "throttleMailVerify") {
		t.Error("绑定入口没有限流了")
	}

	// 三、主机名不能由调用方直接决定。请求体里带 smtpHost/imapHost 是允许
	// 的（「其他」那一档要它），但它必须经过服务端的校验和服务商表——网关
	// 只做转发，判断在 mail 服务的 resolveHosts / validateCustomHost。
	if !strings.Contains(fn, "Provider: body.Provider") {
		t.Error("服务商代号没有传给服务端——主机名就会变成调用方说了算，" +
			"那等于任何员工都能让邮件服务带着凭据去连任意 host:port")
	}

	// 四、不给地址时不能悄悄退回登录地址。那个默认正是老约束的实现方式，
	// 而登录地址现在可能一个信箱都不对应。
	if strings.Contains(fn, "Email: op.Email") {
		t.Error("地址又退回成登录地址了——ERP 账号是 263 的人可以只绑 Gmail，" +
			"这个默认会把他绑到一个不存在的信箱上")
	}
}

// A service saying "you have run out of attempts" has to reach the browser as
// 429 and not 500.
//
// The map in writeGRPCError falls through to InternalServerError for anything
// unlisted, so a code nobody remembered to add turns a precise, actionable
// message into "服务器错误" — the one answer that tells the person nothing and
// invites them to retry immediately, which is the opposite of what was said.
func TestRunningOutOfAttemptsReachesTheBrowserAsTooManyRequests(t *testing.T) {
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "codes.ResourceExhausted: http.StatusTooManyRequests") {
		t.Fatal("a throttled service error still surfaces as an internal error")
	}
}

// 换一串地址换不来新预算。
//
// 这条是把一次审查里的探针钉下来：当时限流键只有「人 + 目标地址」，探针
// 用二十个不同地址各试五次，**一百次真实登录全部打到了邮件服务器上**，
// 而预算写的是「每 15 分钟五次」。地址是调用方给的，所以它一个人就能把
// 预算乘以任意倍数——那不是收紧，是取消。
//
// 用假的 EmailService：这里要证的是网关的计费，不是邮件服务的判断。
func TestChangingTheAddressDoesNotBuyMoreAttempts(t *testing.T) {
	addr := os.Getenv("GATEWAY_TEST_REDIS")
	if addr == "" {
		t.Skip("set GATEWAY_TEST_REDIS")
	}
	stub := &rejectEveryLogin{}
	srv := &Server{
		Throttle: NewFailureThrottle(addr, slog.New(slog.NewTextHandler(io.Discard, nil))),
		Emails:   stub,
		Unlock:   NewUnlockStore(addr, time.Hour),
	}
	tenant, employee := time.Now().UnixNano(), int64(770001)

	// 二十个不同地址，每个试五次。按人那份预算是五次，所以真正打到邮件
	// 服务器上的应该远少于一百次。
	for i := 0; i < 20; i++ {
		for j := 0; j < 5; j++ {
			w := httptest.NewRecorder()
			body := fmt.Sprintf(`{"email":"probe%d@qq.com","secret":"x","provider":"qq"}`, i)
			r := httptest.NewRequest("POST", "/api/mailbox/verify", strings.NewReader(body))
			r = r.WithContext(grpcx.WithOperator(r.Context(),
				grpcx.Operator{TenantID: tenant, EmployeeID: employee, Email: "me@qq.com"}))
			srv.verifyMailbox(w, r)
		}
	}
	// 允许一点余量：两份预算各自的窗口和计数时机不完全同步。但一百次里
	// 打出去十次以内，和"全打出去"是两个量级。
	if stub.calls > 10 {
		t.Errorf("换地址换来了 %d 次真实登录，预算是每人 %d 次——"+
			"限流键带上调用方给的地址，等于把上限乘以地址的个数",
			stub.calls, mailVerifyMaxFailures)
	}
	if stub.calls == 0 {
		t.Error("一次都没打出去，这条测试什么也没证明")
	}
}

// rejectEveryLogin 扮演一个总是拒绝的邮件服务器：每一次都是 HostRejected，
// 也就是"真的花掉了一次登录"，正是该扣预算的那种失败。
type rejectEveryLogin struct {
	mailv1.EmailServiceClient
	calls int
}

func (v *rejectEveryLogin) VerifyMailAccess(_ context.Context, _ *mailv1.VerifyMailAccessRequest, _ ...grpc.CallOption) (*mailv1.VerifyMailAccessResponse, error) {
	v.calls++
	return &mailv1.VerifyMailAccessResponse{Ok: false, HostRejected: true, Detail: "authentication failed"}, nil
}

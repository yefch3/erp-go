package httpapi

import (
	"context"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/grpcx"
)

// 搜索能搜哪些信箱，由**核过的令牌**决定。
//
// 范围是浏览器报上来的（服务端没有「这个人有哪些令牌」的索引，反查不到），
// 所以「报上来的每一把都要回 Redis 核对」是这条口径唯一的支点。松掉的话，
// 报一串假令牌就能读到别人的箱——或者读到自己刚退出的那个箱，而「退出」
// 就只剩一个界面动作。
//
// Run with: GATEWAY_TEST_REDIS=127.0.0.1:6380
func TestSearchScopeOnlyCountsTokensThatCheckOut(t *testing.T) {
	addr := os.Getenv("GATEWAY_TEST_REDIS")
	if addr == "" {
		t.Skip("set GATEWAY_TEST_REDIS to a reachable Redis")
	}
	ctx := context.Background()
	store := NewUnlockStore(addr, time.Minute)
	s := &Server{Unlock: store}
	const tenantID, employeeID = 8801, 8802

	mint := func(accountID int64) string {
		t.Helper()
		tok, _, err := store.Grant(ctx, tenantID, employeeID, accountID)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { store.Revoke(ctx, tenantID, employeeID, tok) })
		return tok
	}
	qq, netease, gmail := mint(11), mint(12), mint(13)

	// 站在 11 号箱上，报上 11/12/13 三把。
	scope := func(primary int64, tokens ...string) []int64 {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/mail-search?keyword=steel", nil)
		req.Header.Set(mailUnlockAllHeader, strings.Join(tokens, ","))
		c := grpcx.WithOperator(req.Context(), grpcx.Operator{
			TenantID: tenantID, EmployeeID: employeeID,
		})
		got := s.unlockedAccountsFor(req.WithContext(withUnlockedAccount(c, primary)))
		sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
		return got
	}

	if got := scope(11, qq, netease, gmail); len(got) != 3 ||
		got[0] != 11 || got[1] != 12 || got[2] != 13 {
		t.Fatalf("三个箱都开着，搜索该覆盖三个：%v", got)
	}

	// 退出 163 之后：那把令牌服务端已经死了，报上来也不算数。
	store.Revoke(ctx, tenantID, employeeID, netease)
	if got := scope(11, qq, netease, gmail); len(got) != 2 ||
		got[0] != 11 || got[1] != 13 {
		t.Fatalf("退出过的箱不该还在搜索范围里：%v", got)
	}

	// 编出来的令牌一律不算。整条请求也不因此失败——手上带着一把过期令牌
	// 是常态，为它整次搜索报错，等于逼人先把每个箱重新登录一遍。
	if got := scope(11, "not-a-real-token", qq); len(got) != 1 || got[0] != 11 {
		t.Fatalf("核不过的令牌该被静静丢掉，当前这个箱仍然要在：%v", got)
	}

	// 当前这把是「不限信箱」（一个箱都没绑的人）：报什么都还是不限。
	if got := scope(accountAll, qq, gmail); got != nil {
		t.Fatalf("不限信箱的令牌不该被这些额外令牌收窄成几个箱：%v", got)
	}

	// 别人的令牌：键里含着身份，核不过。
	other, _, err := store.Grant(ctx, tenantID, employeeID+1, 77)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Revoke(ctx, tenantID, employeeID+1, other)
	if got := scope(11, qq, other); len(got) != 1 || got[0] != 11 {
		t.Fatalf("别人的令牌不该把他的箱带进我的搜索范围：%v", got)
	}
}

// 报上来的令牌数封顶：每一把是一次 Redis 往返，而请求头是调用方给的。
func TestSearchScopeCapsTheNumberOfTokensItChecks(t *testing.T) {
	addr := os.Getenv("GATEWAY_TEST_REDIS")
	if addr == "" {
		t.Skip("set GATEWAY_TEST_REDIS to a reachable Redis")
	}
	ctx := context.Background()
	store := NewUnlockStore(addr, time.Minute)
	s := &Server{Unlock: store}
	const tenantID, employeeID = 8811, 8812

	// 上限之外再多一把真的令牌：它应该被截掉，证明截断发生在核对之前。
	tokens := make([]string, 0, maxUnlockTokensPerRequest+1)
	for i := 0; i < maxUnlockTokensPerRequest; i++ {
		tokens = append(tokens, "pad-not-a-real-token")
	}
	beyond, _, err := store.Grant(ctx, tenantID, employeeID, 42)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Revoke(ctx, tenantID, employeeID, beyond)
	tokens = append(tokens, beyond)

	req := httptest.NewRequest("GET", "/api/mail-search?keyword=steel", nil)
	req.Header.Set(mailUnlockAllHeader, strings.Join(tokens, ","))
	c := grpcx.WithOperator(req.Context(), grpcx.Operator{
		TenantID: tenantID, EmployeeID: employeeID,
	})
	got := s.unlockedAccountsFor(req.WithContext(withUnlockedAccount(c, 11)))
	if len(got) != 1 || got[0] != 11 {
		t.Fatalf("超过上限的那一把该被截掉，只剩当前这个箱：%v", got)
	}
}

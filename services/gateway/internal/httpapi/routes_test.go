package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// 网关是一本地址簿，而它出过一次谁也没看见的错。
//
// GET /api/bank-transactions 被注册了两遍：一遍给收款对账（转出口服务），
// 一遍给银行流水（转采购服务）。两行隔着 144 行，分属两个功能、两次提交。
//
// **chi 对重复注册既不报错也不警告，后注册的静默覆盖先注册的。** 于是收款
// 对账的列表打到了采购的服务上；两边返回的字段名还不一样（采购叫 items，
// 出口叫 transactions），页面拿不到就渲染成空表，并礼貌地显示「没有待处理
// 的流水」。而「登记流水」走 POST，没被覆盖 —— 写进出口的库，读的却是采购
// 的库。全程没有一行错误日志。
//
// scripts/check-duplicate-routes.sh 从源码那一侧挡住重复注册；这里从**跑起来
// 的路由表**这一侧钉住结果：该在的地址一个都不能少。两条一起，才既拦得住
// 「多注册了一条」，也拦得住「改名时漏改了一半」。

// routeSet 走一遍真实构造出来的路由表，收集所有「方法 地址」。
func routeSet(t *testing.T) map[string]bool {
	t.Helper()
	routes, ok := (&Server{}).Router().(chi.Routes)
	if !ok {
		t.Fatal("Router() 返回的东西没法遍历，这条测试失去意义了")
	}
	out := map[string]bool{}
	if err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		// chi 遍历时会把根路径的 / 补在前面，去掉好和源码里写的对得上。
		out[method+" "+strings.TrimSuffix(route, "/")] = true
		return nil
	}); err != nil {
		t.Fatalf("遍历路由表失败：%v", err)
	}
	return out
}

// 收款对账那一组地址必须齐。少一条，页面上就有一个按钮点了没反应。
func TestReceiptRoutesAreAllRegistered(t *testing.T) {
	have := routeSet(t)
	// 和 frontend/src/pages/ReceiptsPage.vue 里调的一一对应。
	want := []string{
		"GET /api/receipt-transactions",                  // 列表
		"GET /api/receipt-transactions/{id}",             // 点开一笔
		"POST /api/receipt-transactions",                 // 「登记流水」
		"POST /api/receipt-transactions/{id}/allocate",   // 核销到合同
		"POST /api/receipt-transactions/{id}/irrelevant", // 「归类」
		"POST /api/receipt-transactions/{id}/reopen",     // 「撤销标记」
		"POST /api/receipt-transactions/{id}/settle",     // 「认差结清」
		"POST /api/receipt-transactions/{id}/unsettle",   // 「撤销结清」
		"POST /api/receivable-due/{id}/receipts",         // 待核销页「记一笔收款」（手填，不连流水）
		"POST /api/contract-receipts/{id}/reverse",       // 冲销记错的那一笔
		"POST /api/receivable-due/{id}/close",            // 「确认核销完成」（转到已完成页）
		"POST /api/receivable-due/{id}/reopen",           // 「撤销完成」（回到待核销页）
		"GET /api/open-receivables",                      // 弹窗里搜合同
		"GET /api/bank-accounts",                         // 「收款账户」
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("收款对账少了这条地址：%s", w)
		}
	}
}

// 银行流水那一组同样要齐，而且**不能和收款对账用同一个地址**。
func TestBankTransactionRoutesAreAllRegistered(t *testing.T) {
	have := routeSet(t)
	// 和 frontend/src/pages/BankTransactionsPage.vue 里调的一一对应。
	want := []string{
		"GET /api/bank-transactions",               // 列表
		"POST /api/bank-transactions",              // 「登记流水」（手工，CSV 之外的入口）
		"POST /api/bank-transactions/import",       // 「导入对账单 CSV」
		"POST /api/bank-transactions/{id}/match",   // 「匹配付款单」
		"POST /api/bank-transactions/{id}/unmatch", // 「取消匹配」
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("银行流水少了这条地址：%s", w)
		}
	}
}

// 这两组地址不许再撞在一起。
//
// 撞了的后果不是 500，是**一个空列表**——远比崩溃难查，因为它看起来像
// 「确实没有数据」。
func TestReceiptsAndBankLedgerDoNotShareAnAddress(t *testing.T) {
	have := routeSet(t)
	for route := range have {
		if !strings.Contains(route, "/api/bank-transactions") {
			continue
		}
		// 银行流水那一组地址底下，不许挂着收款对账才有的动作。
		for _, receiptOnly := range []string{"/allocate", "/irrelevant", "/reopen"} {
			if strings.HasSuffix(route, receiptOnly) {
				t.Errorf("%s 把收款对账的动作挂回了银行流水的地址下——"+
					"这两组必须分开，否则又会互相覆盖", route)
			}
		}
	}
}

// ---------------------------------------------------------------- 我的资料

// 「我的资料」那一组地址必须齐，否则页面上有按钮点了没反应。
func TestMyProfileRoutesAreAllRegistered(t *testing.T) {
	have := routeSet(t)
	// 和 frontend/src/pages/MyProfilePage.vue 里调的一一对应。
	for _, w := range []string{
		"GET /api/me/profile",                     // 打开页面
		"PUT /api/me/profile",                     // 保存电话和英文名
		"POST /api/me/avatar/presign",             // 换一个直传地址
		"POST /api/me/avatar",                     // 传完落库（空 key 表示移除）
		"POST /api/employees/avatar-urls",         // 列表页批量取头像
		"POST /api/employees/{id}/avatar/presign", // 管理员替别人换
		"POST /api/employees/{id}/avatar",         // 同上
		"GET /api/org-chart",                      // 组织架构图
	} {
		if !have[w] {
			t.Errorf("我的资料少了这条地址：%s", w)
		}
	}
}

// middlewareCount 数一条路由挂了几层中间件。
//
// 只用来做**同类对比**，不看绝对值：绝对值会随着全局中间件增减而变，
// 而「这条路由和那条已知的路由挂得一样多吗」不会。
func middlewareCount(t *testing.T, method, route string) int {
	t.Helper()
	routes, ok := (&Server{}).Router().(chi.Routes)
	if !ok {
		t.Fatal("Router() 返回的东西没法遍历")
	}
	n := -1
	if err := chi.Walk(routes, func(m, r string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		if m == method && strings.TrimSuffix(r, "/") == route {
			n = len(mws)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n < 0 {
		t.Fatalf("路由表里没有 %s %s", method, route)
	}
	return n
}

// 「我的资料」不能挂权限门，管理员改别人的必须挂。
//
// 这条钉的是这一组最容易做错的地方。给 /api/me/* 加上 iam:employee:read 是个
// 看起来更安全、实际是错的改动——普通员工**没有**那个权限，加了之后所有人
// 都打不开自己的资料页，而错误信息会是「没有执行此操作的权限」，
// 让人以为是权限配错了，去改角色，越改越远。
//
// 反过来，管理员那两条要是漏了权限门，任何登录的人都能改别人的照片。
//
// 用「和已知路由比」而不是数绝对值：/api/me/permissions 是确定不带权限门的，
// PUT /api/employees/{id} 是确定带的。
func TestMyProfileIsNotBehindAPermissionButTheAdminAvatarRoutesAre(t *testing.T) {
	openBaseline := middlewareCount(t, "GET", "/api/me/permissions")
	gatedBaseline := middlewareCount(t, "PUT", "/api/employees/{id}")
	if openBaseline >= gatedBaseline {
		t.Fatal("基准不成立：带权限门的路由中间件数没有多于不带的，这条测试证明不了任何事")
	}

	for _, r := range []struct{ method, route string }{
		{"GET", "/api/me/profile"},
		{"PUT", "/api/me/profile"},
		{"POST", "/api/me/avatar/presign"},
		{"POST", "/api/me/avatar"},
	} {
		if got := middlewareCount(t, r.method, r.route); got != openBaseline {
			t.Errorf("%s %s 挂了 %d 层中间件，而不带权限门的路由是 %d 层——"+
				"看自己的资料不该需要 iam:employee:read，普通员工根本没有那个权限",
				r.method, r.route, got, openBaseline)
		}
	}

	for _, r := range []struct{ method, route string }{
		{"POST", "/api/employees/{id}/avatar/presign"},
		{"POST", "/api/employees/{id}/avatar"},
		{"POST", "/api/employees/avatar-urls"},
		{"GET", "/api/org-chart"},
	} {
		if got := middlewareCount(t, r.method, r.route); got != gatedBaseline {
			t.Errorf("%s %s 挂了 %d 层中间件，而带权限门的路由是 %d 层——"+
				"改别人的头像必须要权限", r.method, r.route, got, gatedBaseline)
		}
	}
}

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

// 多信箱那一组地址必须齐。少一条，左侧的切换器和「添加邮箱」点了没反应，
// 而且是**静默**的：前端拿到 404，catch 里只把它当成"加载失败"。
func TestMultiMailboxRoutesAreAllRegistered(t *testing.T) {
	have := routeSet(t)
	for _, want := range []string{
		// 我名下的全部信箱。取代了按人取单行的「我的邮箱」——那个在一人
		// 多箱下会随机指向其中一个，而且不报错。
		"GET /api/my-mailboxes",
		// 换写信时默认用哪个箱。
		"POST /api/my-mailboxes/default",
		"POST /api/my-mailboxes/unbind",
		// 绑定/登录邮箱。**存邮箱凭据的路只有这一条**，它要先拿这一对去
		// 邮件服务器真的登录一次，成功了才落库。
		"POST /api/mailbox/verify",
	} {
		if !have[want] {
			t.Errorf("路由没注册：%s", want)
		}
	}
}

func TestExistingContractTakeoverRouteIsRegistered(t *testing.T) {
	if !routeSet(t)["POST /api/contracts/existing"] {
		t.Fatal("录入已有合同的地址没有注册")
	}
}

// 流水删除那一组。少一条，页面上「删除」或「恢复」点了没反应——而且是
// 静默的：前端拿到 404，catch 里只当成一次失败。
func TestBankTransactionDeleteRoutesAreRegistered(t *testing.T) {
	have := routeSet(t)
	for _, want := range []string{
		// 删是归档不是抹掉，理由必填。POST 而不是 DELETE：带 body 的 DELETE
		// 在代理和客户端那层各家实现不一，丢掉 body 的后果是「你明明填了
		// 理由，它说你没填」。
		"POST /api/bank-transactions/{id}/delete",
		// 误删之后重新登记同一笔会被流水号的唯一键挡住，而那行在列表里又
		// 看不见——没有这条路，人只会觉得系统在胡说。
		"POST /api/bank-transactions/{id}/restore",
		// 改一行流水，理由必填、每处改动留痕。
		"PUT /api/bank-transactions/{id}",
		"GET /api/bank-transactions/{id}/changes",
	} {
		if !have[want] {
			t.Errorf("路由没注册：%s", want)
		}
	}
}

// 应收那一组地址必须齐。少一条，页面上就有一个按钮点了没反应。
func TestReceivableRoutesAreAllRegistered(t *testing.T) {
	have := routeSet(t)
	// 和 frontend/src/pages/ReceivableDuePage.vue（待核销 / 已完成两页）
	// 以及 BankTransactionsPage 的「收款账户」一一对应。
	want := []string{
		"GET /api/receivable-due",                  // 两页共用的列表，closed 参数翻面
		"POST /api/receivable-due/{id}/receipts",   // 「记一笔收款」（手填，不连流水）
		"POST /api/contract-receipts/{id}/reverse", // 冲销记错的那一笔
		"POST /api/receivable-due/{id}/close",      // 「确认核销完成」（转到已完成页）
		"POST /api/receivable-due/{id}/reopen",     // 「撤销完成」（回到待核销页）
		"POST /api/receivable-due/{id}/due-date",   // 「改到期日」（合同的常规编辑口只对草稿开放）
		"GET /api/contracts/{id}/receipts",         // 展开行看这张合同的收款明细
		"GET /api/bank-accounts",                   // 「收款账户」（现在在银行流水页上）
		"POST /api/bank-accounts",
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("应收这一组少了这条地址：%s", w)
		}
	}
}

// 「收款对账」那一组地址必须**保持消失**。
//
// 需求变更后核销不再从一行银行流水出发，那一页整个下线了。这些地址是
// 另一条写路径——它写出来的核销行挂着 bank_txn_id，绕开新页面的形状。
// 没有界面却仍能写账的入口，是下一次「数据怎么会变成这样」的起点；
// 哪天有人顺手把某个 handler 重新挂回路由表，这条会拦住。
//
// 老数据里挂着流水的核销行照样能冲销：走 /api/contract-receipts/{id}/reverse，
// 它在服务层转交给带行锁、认差结清守门和认领量回写的那条老路径。
func TestRetiredReceiptQueueRoutesStayGone(t *testing.T) {
	have := routeSet(t)
	for _, gone := range []string{
		"GET /api/receipt-transactions",
		"GET /api/receipt-transactions/{id}",
		"POST /api/receipt-transactions",
		"POST /api/receipt-transactions/{id}/allocate",
		"POST /api/receipt-transactions/{id}/irrelevant",
		"POST /api/receipt-transactions/{id}/reopen",
		"POST /api/receipt-transactions/{id}/settle",
		"POST /api/receipt-transactions/{id}/unsettle",
		"POST /api/receipt-allocations/{id}/reverse",
		"GET /api/open-receivables",
	} {
		if have[gone] {
			t.Errorf("%s 又回来了——收款对账那条写路径是有意撤掉的，"+
				"核销现在只从「待核销」页走 /api/receivable-due/{id}/receipts", gone)
		}
	}
}

// 银行流水那一组同样要齐，而且**不能和收款对账用同一个地址**。
func TestBankTransactionRoutesAreAllRegistered(t *testing.T) {
	have := routeSet(t)
	// 和 frontend/src/pages/BankTransactionsPage.vue 里调的一一对应。
	want := []string{
		"GET /api/bank-transactions",         // 列表
		"POST /api/bank-transactions",        // 「登记流水」（手工，CSV 之外的入口）
		"POST /api/bank-transactions/import", // 「导入对账单 CSV」
		// match / unmatch 已下线，见 TestRetiredSupplierPageRoutesStayGone。
		"POST /api/bank-transactions/{id}/attachment/presign", // 传对账单：要直传地址
		"POST /api/bank-transactions/{id}/attachment",         // 传对账单：传完登记 key
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

// 供应商对账那一组地址必须齐。少一条，页面上就有一个按钮点了没反应。
func TestSupplierReconRoutesAreAllRegistered(t *testing.T) {
	have := routeSet(t)
	// 和 frontend/src/pages/SupplierReconPage.vue（待核销 / 已完成两页）
	// 一一对应。
	want := []string{
		"GET /api/supplier-recon",                                       // 两页共用的列表，view=done 翻面
		"GET /api/supplier-recon/{id}/payments",                         // 展开行看这张采购单的付款明细
		"POST /api/supplier-recon/{id}/payments",                        // 「记一笔付款」（手填，不连流水）
		"POST /api/supplier-recon/{id}/payments/{allocationId}/reverse", // 冲销记错的那一笔
		"POST /api/supplier-recon/{id}/close",                           // 「确认核销完成」（转到已完成页）
		"POST /api/supplier-recon/{id}/reopen",                          // 「撤销完成」（回到待核销页）
		"POST /api/supplier-recon/{id}/due-date",                        // 「改到期日」（已下单的单没有别的编辑入口）
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("供应商对账这一组少了这条地址：%s", w)
		}
	}
}

// 供应商这边只剩「供应商对账」一个界面，别的三组地址必须**保持消失**。
//
// 上一版这里是一条反向的测试：「这次改造唯一要保证『一动不动』的两组……
// 而『不动』如果没人钉，下一次顺手清理就会把它们清掉。」这次就是那个
// 「下一次」——需求收敛成「只留一个和客户对账类似的供应商对账」，那两组
// 连同往来汇总一起下线了。断言跟着翻面，理由也跟着换：
//
// 没有界面却仍然能写账的入口，是下一次「数据怎么会变成这样」的起点。
// 服务端实现和 proto 上的 RPC 都还在（历史数据要读得出、要冲得掉），
// 所以拦住重新挂路由这件事只能靠这条测试。
func TestRetiredSupplierPageRoutesStayGone(t *testing.T) {
	have := routeSet(t)
	for _, gone := range []string{
		// 发票：录入、作废、三单匹配、扫描件。凭证上传搬到了对账页。
		"GET /api/supplier-invoices",
		"GET /api/supplier-invoices/{id}",
		"POST /api/supplier-invoices",
		"POST /api/supplier-invoices/{id}/void",
		"POST /api/supplier-invoices/{id}/match",
		"POST /api/supplier-invoices/{id}/attachment/presign",
		"POST /api/supplier-invoices/{id}/attachment",
		// 付款：建付款单、拆到发票/采购单、冲销。老的核销行改由对账页
		// 在服务层转交冲销（见 supplierrecon.go 的 ReversePOPayment）。
		"GET /api/supplier-payments",
		"GET /api/supplier-payments/{id}",
		"POST /api/supplier-payments",
		"POST /api/supplier-payments/{id}/allocations",
		"POST /api/supplier-payments/allocations/{allocationId}/reverse",
		// 往来汇总：只读，但入口也一起收了。
		"GET /api/supplier-statements",
		"GET /api/supplier-statements/{id}",
		// 银行流水对上付款单：付款单没有创建入口之后这条就是空按钮，
		// 而且和「流水只是记录、不参与核销」这个新模型本来就冲突。
		// **unmatch 不在这里**——解开历史匹配必须留着，否则那些行的归属
		// 从此谁也改不了（改归属的守门会让人「先取消匹配」）。
		"POST /api/bank-transactions/{id}/match",
	} {
		if have[gone] {
			t.Errorf("%s 又回来了——供应商这边只留「供应商对账」一个界面，"+
				"这些地址是有意撤掉的", gone)
		}
	}
}

// 对账页那一组必须齐，凭证那四条也是。少一条，页面上就有一个按钮点了没反应。
func TestSupplierReconFileRoutesAreAllRegistered(t *testing.T) {
	have := routeSet(t)
	for _, want := range []string{
		"GET /api/supplier-recon/{id}/files",
		"POST /api/supplier-recon/{id}/files/presign",
		"POST /api/supplier-recon/{id}/files",
		"POST /api/supplier-recon/files/{fileId}/remove",
	} {
		if !have[want] {
			t.Errorf("凭证这一组少了这条地址：%s", want)
		}
	}
}

// Word / Excel 预览是一条新地址。少了它，前端点「预览」拿到 404，而 catch
// 里只会说一句「预览失败」——看不出是路由没注册还是文件转不了。
func TestOfficeAttachmentPreviewRouteIsRegistered(t *testing.T) {
	const want = "POST /api/inbound-mails/{id}/attachments/{attachmentId}/preview"
	if !routeSet(t)[want] {
		t.Fatalf("缺少路由：%s", want)
	}
}

package httpapi

import (
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

// 网关用 protojson 解请求体，而 protojson **默认拒绝不认识的字段**——不是
// 忽略，是整个请求 400。
//
// 于是「前端加了个字段、proto 还没加」不会表现成那个字段被丢掉，而是表现成
// 整个表单保存不了，错误信息是一句用户看不懂的
// `proto: (line 1:147): unknown field "receivableDueDate"`。这件事真发生过：
// 到期日改成单据字段那一批，前端热更新立刻带上了新字段，而网关容器还是
// 十二小时前的二进制，建合同当场就废了。
//
// 所以这里把浏览器**真正发出去的那些键**钉下来。测的不是「字段存在」——那
// 编译器管得了；测的是「这段 JSON 能被这个 message 收下」，包括 protojson
// 认的是 lowerCamelCase 这件事。
//
// 加前端字段时顺手往这里加一行，比让人在页面上撞见那句英文报错便宜得多。

func mustDecode(t *testing.T, body string, msg proto.Message) {
	t.Helper()
	if err := protojson.Unmarshal([]byte(body), msg); err != nil {
		t.Errorf("这段前端真会发的 JSON 网关收不下：%v\n  body: %s", err, body)
	}
}

func TestBodiesTheBrowserActuallySendsDecode(t *testing.T) {
	// frontend/src/pages/BankTransactionsPage.vue —— 改一条流水。
	// fields 是整个表单对象展开的，少一个 proto 字段就整单保存不了。
	mustDecode(t, `{
		"fields": {
			"direction": "CREDIT", "amount": "100.00", "currency": "USD",
			"txnDate": "2026-08-20", "accountId": "17", "bankRef": "REF-1",
			"counterparty": "ACME", "remittanceInfo": "for EXP-2026-0031",
			"ownership": "OTHER", "ownershipDetail": "运费"
		},
		"reason": "金额多打了一个零"
	}`, &prv1.UpdateBankTransactionRequest{})

	// frontend/src/pages/BankTransactionsPage.vue —— 删一条流水，理由必填。
	mustDecode(t, `{"reason": "同一笔录了两遍，这条是重复的"}`,
		&prv1.DeleteBankTransactionRequest{})
	// 恢复：请求体是空的，路径里带 id。空体也得收得下，否则点「恢复」
	// 直接 400。
	mustDecode(t, `{}`, &prv1.RestoreBankTransactionRequest{})

	// frontend/src/components/MailboxSwitcher.vue —— 设为默认发件箱。
	// accountId 是 lowerCamelCase，proto 里是 account_id；protojson 认前者。
	mustDecode(t, `{"accountId": 7}`, &mailv1.SetDefaultMailboxRequest{})
	// 同一个字段前端也可能发成字符串（int64 在 JSON 里超出安全整数时，
	// proto 的 JSON 映射规定用字符串）。两种都得收得下。
	mustDecode(t, `{"accountId": "7"}`, &mailv1.SetDefaultMailboxRequest{})

	// frontend/src/pages/ContractsPage.vue —— 直接建合同（submitDirect）。
	// terms 是整个表单对象展开的，所以少一个 proto 字段就整单保存不了。
	mustDecode(t, `{
		"customerId": "1", "currency": "USD",
		"terms": {
			"buyerName": "B", "buyerAddress": "", "sellerName": "S", "sellerAddress": "",
			"incoterm": "FOB", "portOfLoading": "", "portOfDischarge": "",
			"paymentMethod": "TT", "deliveryDate": "2026-10-01",
			"receivableDueDate": "2026-11-30", "terms": ""
		},
		"items": []
	}`, &exv1.CreateContractRequest{})

	// 同一页的「保存条款」（saveTerms）走 UpdateContract，terms 同一个形状。
	mustDecode(t, `{
		"terms": {"deliveryDate": "2026-10-01", "receivableDueDate": "", "terms": ""},
		"items": []
	}`, &exv1.UpdateContractRequest{})

	// frontend/src/pages/PurchaseOrdersPage.vue —— 建单和改草稿共用一个
	// payload，前端那边发的是 snake_case，protojson 两种都认。
	mustDecode(t, `{
		"supplier_id": "1", "currency": "CNY", "expected_date": "2026-10-01",
		"payable_due_date": "2026-11-30", "remark": "",
		"fulfillment_mode": "DIRECT_SHIP", "delivery_location_type": "PORT",
		"lines": []
	}`, &prv1.CreateOrderRequest{})
	mustDecode(t, `{
		"supplier_id": "1", "currency": "CNY", "expected_date": "",
		"payable_due_date": "", "lines": []
	}`, &prv1.UpdateOrderRequest{})

	// 两个对账页上的「改到期日」。
	mustDecode(t, `{"dueDate": "2026-11-30", "reason": "重新谈成 60 天"}`,
		&prv1.SetPayableDueDateRequest{})
}

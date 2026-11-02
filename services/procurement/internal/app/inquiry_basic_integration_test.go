package app

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// 询价中销售「保存基本信息」只换轮次令牌，不开新一轮：已提交的报价和报价附件
// 必须原样留下来（id、version 不变），各视图照常看得到、改得了；更早轮次里
// 遗留的报价仍然藏着；撤回仍然清空全部报价。2026-09-25 生产上
// SC-20260919-000067 就是改备注后 9 份报价全部消失。
func TestInquiryBasicInfoKeepsSubmittedQuotes(t *testing.T) {
	customers := newListCustomers()
	s, _, tenant := listTestService(t, &customers)
	s.files = qualityFiles{}
	ctx := context.Background()
	call := func(op int64, in InquiryCommand) InquiryResult {
		t.Helper()
		r, err := s.InquiryWorkspace(ctx, tenant, Operator{ID: op, Name: fmt.Sprintf("op%d", op)}, in)
		if err != nil {
			t.Fatalf("%d %s/%s: %v", op, in.View, in.Action, err)
		}
		return r
	}
	saved := call(101, InquiryCommand{Action: "save", View: "SALES", Body: InquiryBody{Title: "basic info", CustomerID: "10", Customer: "Canonical", Remark: "first remark", Products: []InquiryProduct{{Product: "Steel", Specification: "Q235", Quantity: "3", Unit: "MT"}}}}).Item
	submitted := call(101, InquiryCommand{Action: "submit", View: "SALES", ID: saved.ID, Revision: saved.Revision}).Item
	round := submitted.Revision
	productID := submitted.Body.Products[0].ID

	file := call(201, InquiryCommand{Action: "upload", View: "PROCUREMENT", ID: submitted.ID, Revision: round, FileName: "factory.pdf", FileData: []byte("quote")}).Attachment
	price := InquiryQuoteBody{Company: "Factory", Currency: "USD", QuoteCategory: "FOB_USD", Incoterm: "FOB", Prices: []InquiryPrice{{ProductID: productID, Price: "12.34"}}, Attachments: []InquiryAttachment{*file}}
	procurement := call(201, InquiryCommand{Action: "quote", View: "PROCUREMENT", ID: submitted.ID, Revision: round, Quote: price, Submit: true}).Item.Quotes[0]
	freight := InquiryQuoteBody{Company: "Forwarder", Incoterm: "CFR", Charges: []InquiryCharge{{Name: "Ocean", Amount: "10", Quantity: "1", Currency: "USD"}}}
	logistics := call(301, InquiryCommand{Action: "quote", View: "LOGISTICS", ID: submitted.ID, Revision: round, Quote: freight, Submit: true}).Item.Quotes[0]
	// 修复前留在更早轮次里的报价（生产上那 9 份已经重报过），不能因为这次修复重新冒出来。
	stranded, _ := json.Marshal(price)
	var strandedID string
	if err := s.pool.QueryRow(ctx, `INSERT INTO inquiry_quotes(tenant_id,case_id,inquiry_revision,kind,body,created_by,created_by_name,submitted_at,submitted_by,updated_by,updated_by_name) VALUES($1,$2,$3,'PROCUREMENT',$4,201,'op201',now(),201,201,'op201') RETURNING id::text`, tenant, inquiryID(submitted.ID), round-1, stranded).Scan(&strandedID); err != nil {
		t.Fatal(err)
	}

	edited := call(101, InquiryCommand{Action: "updateBasic", View: "SALES", ID: submitted.ID, Revision: round, Body: InquiryBody{CustomerID: "10", Customer: "Canonical", Delivery: "30 days", Remark: "second remark"}}).Item
	if edited.Revision != round+1 || edited.Body.Remark != "second remark" || edited.Body.Delivery != "30 days" {
		t.Fatalf("basic information not saved: revision %d→%d %+v", round, edited.Revision, edited.Body)
	}

	same := func(what string, got []InquiryQuoteView, want ...InquiryQuoteView) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: %d quotes after saving basic information, want %d: %+v", what, len(got), len(want), got)
		}
		for i := range want {
			if got[i].ID != want[i].ID || got[i].Version != want[i].Version || got[i].UpdatedAt != want[i].UpdatedAt || got[i].UpdatedBy != want[i].UpdatedBy {
				t.Fatalf("%s: quote %d changed: was %s v%d %s %s, now %s v%d %s %s", what, i, want[i].ID, want[i].Version, want[i].UpdatedBy, want[i].UpdatedAt, got[i].ID, got[i].Version, got[i].UpdatedBy, got[i].UpdatedAt)
			}
		}
	}
	same("procurement", call(201, InquiryCommand{Action: "get", View: "PROCUREMENT", ID: submitted.ID}).Item.Quotes, procurement)
	same("logistics", call(301, InquiryCommand{Action: "get", View: "LOGISTICS", ID: submitted.ID}).Item.Quotes, logistics)
	sales := call(101, InquiryCommand{Action: "get", View: "QUOTATIONS", ID: submitted.ID}).Item
	same("quotations", sales.Quotes, procurement, logistics)
	if sales.ProcurementCount != 1 || sales.LogisticsCount != 1 {
		t.Fatalf("counts %d/%d, want 1/1: the stranded quote leaked or a current one went missing", sales.ProcurementCount, sales.LogisticsCount)
	}
	for _, q := range sales.Quotes {
		if q.ID == strandedID {
			t.Fatal("a quote from an older round came back")
		}
	}
	for op, view := range map[int64]string{201: "PROCUREMENT", 301: "LOGISTICS"} {
		list := call(op, InquiryCommand{Action: "list", View: view, State: "QUOTED", Page: 1, Size: 100})
		if list.Total != 1 || list.Items[0].ID != submitted.ID {
			t.Fatalf("%s list no longer shows the inquiry as quoted: %+v", view, list)
		}
	}

	// 改价、附件仍按当前轮次校验。
	price.Prices[0].Price = "13.00"
	again := call(201, InquiryCommand{Action: "quote", View: "PROCUREMENT", ID: submitted.ID, Revision: edited.Revision, QuoteID: procurement.ID, QuoteVersion: procurement.Version, Quote: price, Submit: true}).Item.Quotes[0]
	if again.ID != procurement.ID || again.Version != procurement.Version+1 || again.Body.Prices[0].Price != "13.00" || len(again.Body.Attachments) != 1 {
		t.Fatalf("procurement quote could not be revised in the new round: %+v", again)
	}
	if _, err := s.InquiryWorkspace(ctx, tenant, Operator{ID: 201, Name: "op201"}, InquiryCommand{Action: "quote", View: "PROCUREMENT", ID: submitted.ID, Revision: round, QuoteID: procurement.ID, QuoteVersion: again.Version, Quote: price, Submit: true}); err == nil {
		t.Fatal("a page still on the old revision overwrote the quote")
	}
	var strandedRound int64
	if err := s.pool.QueryRow(ctx, `SELECT inquiry_revision FROM inquiry_quotes WHERE tenant_id=$1 AND id=$2`, tenant, inquiryID(strandedID)).Scan(&strandedRound); err != nil || strandedRound != round-1 {
		t.Fatalf("stranded quote moved to round %d: %v", strandedRound, err)
	}

	withdrawn := call(101, InquiryCommand{Action: "withdraw", View: "SALES", ID: submitted.ID, Revision: edited.Revision}).Item
	var left int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM inquiry_quotes WHERE tenant_id=$1 AND case_id=$2`, tenant, inquiryID(submitted.ID)).Scan(&left); err != nil || left != 0 || withdrawn.State != "WITHDRAWN" {
		t.Fatalf("withdrawal left %d quotes (state %s): %v", left, withdrawn.State, err)
	}
}

// 老询盘的 inquiry_body 为空。保存基本信息要像保存报价一样先补出整张询盘，
// 而不是报内部错误；只存了客户名的老询盘也不能在行锁里卡住。
func TestInquiryBasicInfoOnLegacyInquiry(t *testing.T) {
	customers := newListCustomers()
	s, _, tenant := listTestService(t, &customers)
	ctx := context.Background()
	legacy, err := s.CreateSourcingCase(ctx, tenant, NewSourcingCase{CustomerID: 10, CustomerName: "Canonical", Lines: []SourcingLineInput{{Product: "Legacy coil", Quantity: "2", QuantityUnit: "MT", Port: "Houston"}}}, Operator{ID: 101, Name: "op101"})
	if err != nil {
		t.Fatal(err)
	}
	id := legacy.Head.ID
	// 工作台之前就已在询价中的询盘：没有 inquiry_body，也可能只存了客户名。
	if _, err = s.pool.Exec(ctx, `UPDATE sourcing_cases SET status='REVIEWING',customer_id=0,customer_name='Canonical' WHERE tenant_id=$1 AND id=$2`, tenant, id); err != nil {
		t.Fatal(err)
	}
	var revision int64
	if err = s.pool.QueryRow(ctx, `SELECT inquiry_revision FROM sourcing_cases WHERE tenant_id=$1 AND id=$2 AND inquiry_body IS NULL`, tenant, id).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r, err := s.InquiryWorkspace(bounded, tenant, Operator{ID: 101, Name: "op101"}, InquiryCommand{Action: "updateBasic", View: "SALES", ID: fmt.Sprint(id), Revision: revision, Body: InquiryBody{CustomerID: "11", Customer: "Second", DestinationPort: "Long Beach", Remark: "legacy remark"}})
	if err != nil {
		t.Fatalf("updateBasic on a legacy inquiry: %v", err)
	}
	v := r.Item
	if v.Legacy || v.Body.Remark != "legacy remark" || v.Body.CustomerID != "11" || v.Body.Customer != "Second" || v.Body.DestinationPort != "Long Beach" {
		t.Fatalf("legacy inquiry not saved as edited: %+v", v.Body)
	}
	if len(v.Body.Products) != 1 || v.Body.Products[0].Product != "Legacy coil" || v.Body.Products[0].ID != fmt.Sprint(legacy.Lines[0].ID) {
		t.Fatalf("legacy products lost or renumbered: %+v", v.Body.Products)
	}
}

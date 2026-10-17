package app

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

func TestMailIntakeProductPreservesLongDescriptionAndSupplementalFields(t *testing.T) {
	description := strings.Repeat("PPGL ASTM A792 CS-B / AZ25 WITH GUARD FILM; ", 15) + "SKU 102646"
	product := inquiryProductFromSourcingLine(store.ListSourcingLinesRow{ID: 1, Product: description, Quantity: "470", QuantityUnit: "MT"}, map[string]string{"specification": "0.40 X 1220MM", "weight": "470", "custom.color": "RAL 3020"})
	if product.Product != description || product.Specification != "0.40 X 1220MM" || product.Weight != "470" || product.CustomFields["custom.color"] != "RAL 3020" {
		t.Fatalf("imported product lost data: %#v", product)
	}
}

func TestMailIntakeSummaryAndProductsRoundTrip(t *testing.T) {
	s, _, tenant := listTestService(t, nil)
	ctx := context.Background()
	summary := "客户询价包含10项产品，明细总量3580 MT。CFR CALLAO (USD/MT). Payment Terms: CAD or LC at sight. Shipment (LSD): 30Dic; 包装及标识要求需按来源执行。"
	description := strings.Repeat("PPGL ASTM A792 CS-B / AZ25 WITH GUARD FILM; ", 15) + "SKU 102646"
	lines := make([]SourcingLineInput, 10)
	for i := range lines {
		lines[i] = SourcingLineInput{Product: description, Quantity: "358", QuantityUnit: "MT", CustomFields: map[string]string{"specification": "0.40 X 1220MM"}}
	}
	created, err := s.CreateSourcingCase(ctx, tenant, NewSourcingCase{Title: "客户询价", Remark: summary, Lines: lines}, Operator{ID: 101, Name: "Sales"})
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(created.Head.ID, 10)
	read := func() *InquiryView {
		t.Helper()
		out, err := s.InquiryWorkspace(ctx, tenant, Operator{ID: 101}, InquiryCommand{Action: "get", View: "SALES", ID: id})
		if err != nil {
			t.Fatal(err)
		}
		if out.Item.Body.Remark != summary || len(out.Item.Body.Products) != 10 {
			t.Fatalf("lost intake content: %#v", out.Item.Body)
		}
		for _, product := range out.Item.Body.Products {
			if product.Product != description || product.Specification != "0.40 X 1220MM" {
				t.Fatalf("lost product: %#v", product)
			}
		}
		return out.Item
	}
	current := read()
	_, err = s.InquiryWorkspace(ctx, tenant, Operator{ID: 101}, InquiryCommand{Action: "save", View: "SALES", ID: id, Revision: current.Revision, Body: current.Body})
	if err != nil {
		t.Fatal(err)
	}
	read()
}

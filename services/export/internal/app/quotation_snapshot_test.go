package app

import (
	"context"
	"testing"
)

type snapshotCustomerStub struct{}

func (snapshotCustomerStub) Get(context.Context, int64) (Customer, error) {
	return Customer{ID: 7, Name: "测试客户", Status: "ACTIVE"}, nil
}

type snapshotProductStub struct {
	called bool
}

func (p *snapshotProductStub) Get(context.Context, int64) (Product, error) {
	p.called = true
	return Product{}, nil
}

// 批量那条路也要置位——不然「未绑定内部产品时不应调用产品目录」这条断言
// 会因为改用批量而悄悄失效：它还在看 Get，而代码已经走 GetMany 了。
func (p *snapshotProductStub) GetMany(context.Context, []int64) (map[int64]Product, error) {
	p.called = true
	return map[int64]Product{}, nil
}

// 未启用产品模块时，已审核询盘的名称、规格和单位快照也能生成报价明细。
func TestResolveQuotationAcceptsReviewedProductSnapshot(t *testing.T) {
	products := &snapshotProductStub{}
	s := New(nil, Deps{Customers: snapshotCustomerStub{}, Products: products})

	_, lines, total, err := s.resolve(context.Background(), QuotationInput{
		CustomerID: 7,
		Items: []ItemInput{{
			ProductName: "热镀锌钢卷",
			UomCode:     "MT",
			Spec:        "ASTM A653",
			Qty:         "40",
			UnitPrice:   "125.50",
		}},
	})
	if err != nil {
		t.Fatalf("resolve snapshot quotation: %v", err)
	}
	if products.called {
		t.Fatal("未绑定内部产品时不应调用产品目录")
	}
	if len(lines) != 1 || lines[0].product.Name != "热镀锌钢卷" || lines[0].product.UomCode != "MT" {
		t.Fatalf("unexpected snapshot line: %#v", lines)
	}
	if got := total.StringFixed(2); got != "5020.00" {
		t.Fatalf("total=%s, want 5020.00", got)
	}
}

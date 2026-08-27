package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/sgao19/erp-go/pkg/apierr"
)

// countingProducts 记下被问了几次、每次问了哪些 id。
//
// 这个测试和别的不一样：它盯的**不是结果对不对，是问了几次**。结果对不对
// 换回一行一问也照样成立，所以只断言结果的话，这个改动可以被悄悄退回去而
// 没有任何测试变红。
type countingProducts struct {
	mu sync.Mutex
	// 单条查询的次数——改完之后这里必须是 0。
	singleCalls int
	// 每一次批量查询问了哪些 id，用来验证去重是不是真的做了。
	batches [][]int64
	catalog map[int64]Product
}

func newCountingProducts(ps ...Product) *countingProducts {
	m := make(map[int64]Product, len(ps))
	for _, p := range ps {
		m[p.ID] = p
	}
	return &countingProducts{catalog: m}
}

func (c *countingProducts) Get(_ context.Context, id int64) (Product, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.singleCalls++
	p, ok := c.catalog[id]
	if !ok {
		return Product{}, apierr.NotFound("PD_NOT_FOUND", "产品不存在")
	}
	return p, nil
}

func (c *countingProducts) GetMany(_ context.Context, ids []int64) (map[int64]Product, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.batches = append(c.batches, append([]int64(nil), ids...))
	out := make(map[int64]Product, len(ids))
	for _, id := range ids {
		if p, ok := c.catalog[id]; ok {
			out[id] = p
		}
	}
	return out, nil
}

type batchCustomerStub struct{}

func (batchCustomerStub) Get(context.Context, int64) (Customer, error) {
	return Customer{ID: 7, Name: "测试客户", Status: "ACTIVE", Currency: "USD"}, nil
}

// 一张多行的报价单只问一次产品服务，不是一行一次。
//
// 原来是每行一次跨服务往返——一张 30 行的合同就是 30 次，每一次都在等网络。
func TestQuotationAsksProductServiceOnce(t *testing.T) {
	products := newCountingProducts(
		Product{ID: 11, Code: "P-11", Name: "热镀锌钢卷", UomCode: "MT", Status: "ACTIVE"},
		Product{ID: 12, Code: "P-12", Name: "冷轧板", UomCode: "MT", Status: "ACTIVE"},
	)
	s := New(nil, Deps{Customers: batchCustomerStub{}, Products: products})

	// 六行，用到两个产品——**同一个产品出现三次**，去重之后只该问两个 id。
	items := make([]ItemInput, 0, 6)
	for i := 0; i < 3; i++ {
		items = append(items,
			ItemInput{ProductID: 11, Qty: "10", UnitPrice: "100"},
			ItemInput{ProductID: 12, Qty: "5", UnitPrice: "200"},
		)
	}
	_, lines, total, err := s.resolve(context.Background(), QuotationInput{CustomerID: 7, Items: items})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(lines) != 6 {
		t.Fatalf("应该有 6 行，实际 %d", len(lines))
	}
	if got := total.StringFixed(2); got != "6000.00" {
		t.Fatalf("合计 %s，想要 6000.00", got)
	}

	// **这两条才是这个测试的理由。**
	if products.singleCalls != 0 {
		t.Fatalf("不该再按行单问了，实际问了 %d 次", products.singleCalls)
	}
	if len(products.batches) != 1 {
		t.Fatalf("应该只问一次，实际问了 %d 次：%v", len(products.batches), products.batches)
	}
	if len(products.batches[0]) != 2 {
		t.Fatalf("六行两个产品，去重之后该问 2 个 id，实际 %v", products.batches[0])
	}
}

// 产品不存在时要说得出**是第几行**。
//
// 一张三十行的单子退回来只说「产品不存在」，人得自己一行行找。批量之后这条
// 更要紧：所有行是一起查的，不带行号就真的无从下手了。
func TestMissingProductNamesTheLine(t *testing.T) {
	products := newCountingProducts(
		Product{ID: 11, Code: "P-11", Name: "热镀锌钢卷", UomCode: "MT", Status: "ACTIVE"},
	)
	s := New(nil, Deps{Customers: batchCustomerStub{}, Products: products})

	_, _, _, err := s.resolve(context.Background(), QuotationInput{
		CustomerID: 7,
		Items: []ItemInput{
			{ProductID: 11, Qty: "1", UnitPrice: "1"},
			{ProductID: 11, Qty: "1", UnitPrice: "1"},
			{ProductID: 99, Qty: "1", UnitPrice: "1"}, // 第 3 行，不存在
		},
	})
	if err == nil {
		t.Fatal("产品不存在应该报错")
	}
	// 错误码沿用产品服务原来那个，所以网关映射的状态和前端认的码都不变。
	if code := apierr.CodeFromError(err); code != "PD_NOT_FOUND" {
		t.Fatalf("错误码应该还是 PD_NOT_FOUND，实际 %q", code)
	}
	var ae *apierr.Error
	if !errors.As(err, &ae) || ae.Meta["line"] != "3" {
		t.Fatalf("要说清楚是第 3 行，实际 %v", err)
	}
}

// 停用的产品要能被认出来，而不是变成「不存在」。
//
// 批量查询**不按状态过滤**就是为了这个：在产品服务那边筛掉停用的，这里拿到
// 的就是「查不到」，报出去的话会变成「产品不存在」——一句让人去查产品主数据、
// 结果什么都查不到的错话。
func TestInactiveProductIsNotReportedAsMissing(t *testing.T) {
	products := newCountingProducts(
		Product{ID: 11, Code: "P-11", Name: "停用了的", UomCode: "MT", Status: "INACTIVE"},
	)
	s := New(nil, Deps{Customers: batchCustomerStub{}, Products: products})

	_, _, _, err := s.resolve(context.Background(), QuotationInput{
		CustomerID: 7,
		Items:      []ItemInput{{ProductID: 11, Qty: "1", UnitPrice: "1"}},
	})
	if err == nil {
		t.Fatal("停用产品应该被拒绝")
	}
	if !strings.Contains(err.Error(), "已停用") {
		t.Fatalf("要说「已停用」而不是「不存在」，实际 %v", err)
	}
	var ae *apierr.Error
	if !errors.As(err, &ae) || ae.Meta["product"] != "P-11" {
		t.Fatalf("要带上产品编码好让人找得到，实际 %v", err)
	}
}

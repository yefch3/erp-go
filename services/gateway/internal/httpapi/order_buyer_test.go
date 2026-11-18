package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"google.golang.org/grpc"
)

// 历史补录单的采购员：候选名单和换人（#643）。
//
// 规则都在采购服务里（谁看得见这张单、谁接得住）。网关这一层能出的错是另外几样，
// 而且都不响：地址没注册上；「buyer-candidates」被当成一张单的编号送去查详情；
// 请求体里夹带的编号盖过了地址上的那张单；没值的字段从响应里消失，页面读到
// undefined。这里一样一样钉住。

type orderBuyerClientStub struct {
	prv1.PurchaseOrderServiceClient
	listed  int
	changed *prv1.ChangeOrderBuyerRequest
	answer  *prv1.ChangeOrderBuyerResponse
	err     error
}

func (c *orderBuyerClientStub) ListOrderBuyerCandidates(context.Context, *prv1.ListOrderBuyerCandidatesRequest, ...grpc.CallOption) (*prv1.ListOrderBuyerCandidatesResponse, error) {
	c.listed++
	if c.err != nil {
		return nil, c.err
	}
	return &prv1.ListOrderBuyerCandidatesResponse{Candidates: []*prv1.OrderBuyerCandidate{
		{EmployeeId: 19, Name: "张晓雅", DepartmentName: "天津"},
		{EmployeeId: 27, Name: "王燕楠"},
	}}, nil
}

func (c *orderBuyerClientStub) ChangeOrderBuyer(_ context.Context, in *prv1.ChangeOrderBuyerRequest, _ ...grpc.CallOption) (*prv1.ChangeOrderBuyerResponse, error) {
	c.changed = in
	if c.err != nil {
		return nil, c.err
	}
	return c.answer, nil
}

func TestOrderBuyerRoutesAreRegistered(t *testing.T) {
	have := routeSet(t)
	for _, want := range []string{
		"GET /api/purchase-orders/buyer-candidates",
		"PUT /api/purchase-orders/{id}/buyer",
	} {
		if !have[want] {
			t.Errorf("路由表里没有 %s：页面上的采购员下拉会取不到名单，或者换人点了没反应", want)
		}
	}
}

// 候选名单的地址和「一张单的详情」只差最后一段。走错了不会报错——详情那边
// 把 "buyer-candidates" 解析成 0 号单，回一句「采购单不存在」，页面上就是
// 下拉取不到名单、退回成灰色的只读框，和这个功能上线之前一模一样。
func TestOrderBuyerCandidatesIsNotMistakenForAnOrderID(t *testing.T) {
	routes, ok := (&Server{}).Router().(chi.Routes)
	if !ok {
		t.Fatal("Router() 返回的东西没法匹配，这条测试失去意义了")
	}
	for _, tc := range []struct{ method, path, pattern string }{
		{http.MethodGet, "/api/purchase-orders/buyer-candidates", "/api/purchase-orders/buyer-candidates"},
		{http.MethodGet, "/api/purchase-orders/501", "/api/purchase-orders/{id}"},
		{http.MethodPut, "/api/purchase-orders/501/buyer", "/api/purchase-orders/{id}/buyer"},
	} {
		rctx := chi.NewRouteContext()
		if !routes.Match(rctx, tc.method, tc.path) {
			t.Errorf("%s %s 没有匹配到任何路由", tc.method, tc.path)
			continue
		}
		if got := rctx.RoutePattern(); got != tc.pattern {
			t.Errorf("%s %s 走到了 %q，应该是 %q", tc.method, tc.path, got, tc.pattern)
		}
	}
}

func TestOrderBuyerCandidatesKeepIDsAsStrings(t *testing.T) {
	orders := &orderBuyerClientStub{}
	rec := httptest.NewRecorder()
	(&Server{Orders: orders}).listOrderBuyerCandidates(rec, httptest.NewRequest(http.MethodGet, "/api/purchase-orders/buyer-candidates", nil))
	if rec.Code != http.StatusOK || orders.listed != 1 {
		t.Fatalf("status=%d listed=%d body=%s", rec.Code, orders.listed, rec.Body.String())
	}
	var got struct {
		Success bool `json:"success"`
		Data    struct {
			Candidates []struct {
				// 页面拿编号当下拉的值、再原样发回来；它必须是字符串，也必须和
				// 单子上的 buyerId 是同一种写法，不然「现在这位」在名单里对不上。
				EmployeeID     string  `json:"employeeId"`
				Name           string  `json:"name"`
				DepartmentName *string `json:"departmentName"`
			} `json:"candidates"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应不是预期的形状：%v\n%s", err, rec.Body.String())
	}
	if !got.Success || len(got.Data.Candidates) != 2 {
		t.Fatalf("candidates = %+v", got.Data.Candidates)
	}
	first, second := got.Data.Candidates[0], got.Data.Candidates[1]
	if first.EmployeeID != "19" || first.Name != "张晓雅" || first.DepartmentName == nil || *first.DepartmentName != "天津" {
		t.Errorf("first candidate = %+v", first)
	}
	// 没有部门也要有这个字段（空串），不是缺省。
	if second.EmployeeID != "27" || second.DepartmentName == nil || *second.DepartmentName != "" {
		t.Errorf("second candidate = %+v", second)
	}
}

func TestChangeOrderBuyerForwardsThePathOrder(t *testing.T) {
	call := func(orders *orderBuyerClientStub, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPut, "/api/purchase-orders/501/buyer", strings.NewReader(body))
		rc := chi.NewRouteContext()
		rc.URLParams.Add("id", "501")
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
		rec := httptest.NewRecorder()
		(&Server{Orders: orders}).changeOrderBuyer(rec, r)
		return rec
	}

	t.Run("地址上的那张单说了算，请求体里夹带的编号不作数", func(t *testing.T) {
		orders := &orderBuyerClientStub{answer: &prv1.ChangeOrderBuyerResponse{BuyerId: 19, BuyerName: "张晓雅", Changed: true}}
		rec := call(orders, `{"id":"999","buyerId":"19","reason":"按实际负责人分配"}`)
		if rec.Code != http.StatusOK || orders.changed == nil {
			t.Fatalf("status=%d forwarded=%v body=%s", rec.Code, orders.changed != nil, rec.Body.String())
		}
		if orders.changed.GetId() != 501 || orders.changed.GetBuyerId() != 19 || orders.changed.GetReason() != "按实际负责人分配" {
			t.Fatalf("forwarded %+v", orders.changed)
		}
		if body := rec.Body.String(); !strings.Contains(body, `"buyerId":"19"`) || !strings.Contains(body, `"buyerName":"张晓雅"`) || !strings.Contains(body, `"changed":true`) {
			t.Fatalf("body=%s", body)
		}
	})

	t.Run("没换成也要把 changed 写出来，页面靠它区分「已换给」和「没有变化」", func(t *testing.T) {
		orders := &orderBuyerClientStub{answer: &prv1.ChangeOrderBuyerResponse{BuyerId: 27, BuyerName: "王燕楠"}}
		rec := call(orders, `{"buyerId":"27"}`)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"changed":false`) {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("请求体读不懂就不往下传", func(t *testing.T) {
		orders := &orderBuyerClientStub{}
		rec := call(orders, `{"buyerId":`)
		if rec.Code != http.StatusBadRequest || orders.changed != nil {
			t.Fatalf("status=%d forwarded=%v body=%s", rec.Code, orders.changed != nil, rec.Body.String())
		}
	})

	t.Run("采购服务拒绝时，原因原样交给页面", func(t *testing.T) {
		for _, tc := range []struct {
			err    error
			status int
			code   string
		}{
			{apierr.ToStatus(apierr.NotFound("PO_ORDER_NOT_FOUND", "采购单不存在")), http.StatusNotFound, "PO_ORDER_NOT_FOUND"},
			{apierr.ToStatus(apierr.Invalid("PO_BUYER_NOT_ELIGIBLE", "所选同事已停用、没有登录账号，或没有维护采购订单的权限，不能担任采购员")), http.StatusBadRequest, "PO_BUYER_NOT_ELIGIBLE"},
			{apierr.ToStatus(apierr.Conflict("PO_BUYER_HISTORICAL_ONLY", "只有历史补录的采购单可以更换采购员")), http.StatusConflict, "PO_BUYER_HISTORICAL_ONLY"},
		} {
			rec := call(&orderBuyerClientStub{err: tc.err}, `{"buyerId":"19"}`)
			if rec.Code != tc.status || !strings.Contains(rec.Body.String(), `"`+tc.code+`"`) {
				t.Errorf("%s: status=%d body=%s", tc.code, rec.Code, rec.Body.String())
			}
		}
	})
}

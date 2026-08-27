package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"google.golang.org/grpc"
)

type activeCustomerClientStub struct {
	mdv1.CustomerServiceClient
	status   string
	contacts []*mdv1.Contact
}

func (s activeCustomerClientStub) GetCustomer(context.Context, *mdv1.GetCustomerRequest, ...grpc.CallOption) (*mdv1.GetCustomerResponse, error) {
	return &mdv1.GetCustomerResponse{Customer: &mdv1.Customer{Id: 7, Name: "测试客户", Status: s.status}}, nil
}
func (s activeCustomerClientStub) ListCustomerContacts(context.Context, *mdv1.ListCustomerContactsRequest, ...grpc.CallOption) (*mdv1.ListCustomerContactsResponse, error) {
	return &mdv1.ListCustomerContactsResponse{Contacts: s.contacts}, nil
}

type activeSupplierClientStub struct {
	mdv1.SupplierServiceClient
	status        string
	businessTypes []string
}

func (s activeSupplierClientStub) GetSupplier(context.Context, *mdv1.GetSupplierRequest, ...grpc.CallOption) (*mdv1.GetSupplierResponse, error) {
	return &mdv1.GetSupplierResponse{Supplier: &mdv1.Supplier{Id: 8, Name: "测试供应商", Status: s.status, BusinessTypes: s.businessTypes}}, nil
}

// TestResolveActiveMasterdata 验证新业务只能引用启用的客户和供应商。
func TestResolveActiveMasterdata(t *testing.T) {
	t.Run("启用客户可用", func(t *testing.T) {
		s := &Server{Customers: activeCustomerClientStub{status: "ACTIVE"}}
		if _, err := s.resolveActiveCustomer(context.Background(), 7); err != nil {
			t.Fatalf("resolve active customer: %v", err)
		}
	})
	t.Run("停用客户被拒绝", func(t *testing.T) {
		s := &Server{Customers: activeCustomerClientStub{status: "INACTIVE"}}
		if _, err := s.resolveActiveCustomer(context.Background(), 7); err == nil {
			t.Fatal("expected inactive customer error")
		}
	})
	t.Run("启用供应商可用", func(t *testing.T) {
		s := &Server{Suppliers: activeSupplierClientStub{status: "ACTIVE"}}
		if _, err := s.resolveActiveSupplier(context.Background(), 8); err != nil {
			t.Fatalf("resolve active supplier: %v", err)
		}
	})
	t.Run("停用供应商被拒绝", func(t *testing.T) {
		s := &Server{Suppliers: activeSupplierClientStub{status: "INACTIVE"}}
		if _, err := s.resolveActiveSupplier(context.Background(), 8); err == nil {
			t.Fatal("expected inactive supplier error")
		}
	})
}

// TestResolveActiveCustomerContact 验证上传询盘只能引用所选客户名下的有效且有邮箱联系人。
func TestResolveActiveCustomerContact(t *testing.T) {
	contacts := []*mdv1.Contact{
		{Id: 11, Name: "王经理", Email: "wang@example.com", Status: "ACTIVE"},
		{Id: 12, Name: "停用联系人", Email: "old@example.com", Status: "INACTIVE"},
		{Id: 13, Name: "缺少邮箱", Status: "ACTIVE"},
	}
	s := &Server{Customers: activeCustomerClientStub{status: "ACTIVE", contacts: contacts}}

	t.Run("有效联系人返回权威快照", func(t *testing.T) {
		customer, contact, err := s.resolveActiveCustomerContact(context.Background(), 7, 11)
		if err != nil {
			t.Fatalf("resolve contact: %v", err)
		}
		if customer.GetName() != "测试客户" || contact.GetName() != "王经理" || contact.GetEmail() != "wang@example.com" {
			t.Fatalf("unexpected snapshots: customer=%q contact=%q email=%q", customer.GetName(), contact.GetName(), contact.GetEmail())
		}
	})

	t.Run("不属于客户或停用联系人被拒", func(t *testing.T) {
		for _, contactID := range []int64{12, 99} {
			if _, _, err := s.resolveActiveCustomerContact(context.Background(), 7, contactID); err == nil {
				t.Fatalf("contact %d should be rejected", contactID)
			}
		}
	})

	t.Run("缺少邮箱被拒", func(t *testing.T) {
		if _, _, err := s.resolveActiveCustomerContact(context.Background(), 7, 13); err == nil {
			t.Fatal("contact without email should be rejected")
		}
	})

	t.Run("未选择联系人被拒", func(t *testing.T) {
		if _, _, err := s.resolveActiveCustomerContact(context.Background(), 7, 0); err == nil {
			t.Fatal("missing contact should be rejected")
		}
	})
}

type captureSourcingClientStub struct {
	prv1.SourcingServiceClient
	got *prv1.CreateCaseRequest
}

func (c *captureSourcingClientStub) CreateCase(_ context.Context, in *prv1.CreateCaseRequest, _ ...grpc.CallOption) (*prv1.CreateCaseResponse, error) {
	c.got = in
	return &prv1.CreateCaseResponse{SourcingCase: &prv1.SourcingCase{Id: 1, CaseNo: "SC-0001"}}, nil
}

func postSourcingCase(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/sourcing-cases", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.createSourcingCase(rec, req)
	return rec
}

// 询盘必须落在一个真客户身上。
//
// 邮件那条入口曾经只传一个发件人显示名，于是每次都撞在「请选择有效客户」
// 上——按钮在，路不通。这里钉住两头：没客户要挡下，有客户则以主数据的名字
// 为准，不用调用方传来的那个。
func TestCreateSourcingCaseRequiresRealCustomer(t *testing.T) {
	t.Run("没有客户被拒", func(t *testing.T) {
		sourcing := &captureSourcingClientStub{}
		s := &Server{Customers: activeCustomerClientStub{status: "ACTIVE"}, Sourcing: sourcing}
		rec := postSourcingCase(t, s, `{"title":"客户询价单","customerName":"邮件里的发件人"}`)
		if rec.Code == http.StatusOK {
			t.Fatal("只有一个名字不该建得成询盘")
		}
		if !strings.Contains(rec.Body.String(), "MASTERDATA_CUSTOMER_REQUIRED") {
			t.Fatalf("该报缺客户，实际 %s", rec.Body.String())
		}
		if sourcing.got != nil {
			t.Fatal("挡下来的请求不该到达采购服务")
		}
	})

	t.Run("客户名以主数据为准", func(t *testing.T) {
		sourcing := &captureSourcingClientStub{}
		s := &Server{Customers: activeCustomerClientStub{status: "ACTIVE"}, Sourcing: sourcing}
		rec := postSourcingCase(t, s,
			`{"title":"客户询价单","customerId":"7","customerName":"浏览器传来的旧名字"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("选了启用客户就该建得成，实际 %d %s", rec.Code, rec.Body.String())
		}
		if sourcing.got.GetCustomerId() != 7 {
			t.Fatalf("客户 id 该原样传下去，实际 %d", sourcing.got.GetCustomerId())
		}
		if sourcing.got.GetCustomerName() != "测试客户" {
			t.Fatalf("名字该来自主数据而不是调用方，实际 %q", sourcing.got.GetCustomerName())
		}
	})

	t.Run("停用客户被拒", func(t *testing.T) {
		sourcing := &captureSourcingClientStub{}
		s := &Server{Customers: activeCustomerClientStub{status: "INACTIVE"}, Sourcing: sourcing}
		rec := postSourcingCase(t, s, `{"title":"客户询价单","customerId":"7"}`)
		if rec.Code == http.StatusOK {
			t.Fatal("停用客户不能用于新业务")
		}
		if sourcing.got != nil {
			t.Fatal("挡下来的请求不该到达采购服务")
		}
	})
}

// TestScheduleCarrierRole 验证船期的承运方必须真是船公司或货代（B3）：
// 主数据里的业务类型是唯一的角色事实，一家钢厂不能被选成船公司。
func TestScheduleCarrierRole(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/shipping/schedules", nil)
	t.Run("钢厂被拒", func(t *testing.T) {
		s := &Server{Suppliers: activeSupplierClientStub{status: "ACTIVE", businessTypes: []string{"MATERIAL"}}}
		in := &shippingv1.ScheduleInput{CarrierId: 8}
		if err := s.resolveShippingMasterdata(req, in, false); err == nil {
			t.Fatal("a supplier without CARRIER/FORWARDER role must be refused")
		}
	})
	t.Run("货代放行并快照名称", func(t *testing.T) {
		s := &Server{Suppliers: activeSupplierClientStub{status: "ACTIVE", businessTypes: []string{"FORWARDER", "WAREHOUSE"}}}
		in := &shippingv1.ScheduleInput{CarrierId: 8, CarrierForwarder: "浏览器传来的旧名字"}
		if err := s.resolveShippingMasterdata(req, in, false); err != nil {
			t.Fatalf("forwarder should pass: %v", err)
		}
		if in.CarrierForwarder != "测试供应商" {
			t.Fatalf("the snapshot must come from master data, got %q", in.CarrierForwarder)
		}
	})
}

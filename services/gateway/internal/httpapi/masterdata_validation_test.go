package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"google.golang.org/grpc"
)

type activeCustomerClientStub struct {
	mdv1.CustomerServiceClient
	status string
}

func (s activeCustomerClientStub) GetCustomer(context.Context, *mdv1.GetCustomerRequest, ...grpc.CallOption) (*mdv1.GetCustomerResponse, error) {
	return &mdv1.GetCustomerResponse{Customer: &mdv1.Customer{Id: 7, Name: "测试客户", Status: s.status}}, nil
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

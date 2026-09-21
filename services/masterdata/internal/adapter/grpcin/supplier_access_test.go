package grpcin

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/masterdata/internal/app"
	"google.golang.org/grpc"
)

type supplierIAM struct {
	iamv1.AccessServiceClient
	failure error
}

func (s supplierIAM) VisibleEmployees(ctx context.Context, req *iamv1.VisibleEmployeesRequest, _ ...grpc.CallOption) (*iamv1.VisibleEmployeesResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if req.Module != "supplier" || req.EmployeeId != op.EmployeeID {
		return nil, errors.New("wrong supplier scope identity")
	}
	return &iamv1.VisibleEmployeesResponse{All: op.EmployeeID == 99}, s.failure
}

func TestSupplierAccessFailsClosedAndRestrictsDeletion(t *testing.T) {
	h := New(nil)
	called := false
	next := func(context.Context, any) (any, error) {
		called = true
		return nil, nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/erp.masterdata.v1.SupplierService/DeactivateSupplier"}
	if _, err := h.SupplierAccess(supplierIAM{})(context.Background(), &mdv1.DeactivateSupplierRequest{Id: 1}, info, next); err == nil {
		t.Fatal("missing identity accepted")
	}
	ordinary := grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: 1, EmployeeID: 1})
	if _, err := h.SupplierAccess(supplierIAM{failure: errors.New("IAM unavailable")})(ordinary, &mdv1.ListSuppliersRequest{}, info, next); err == nil {
		t.Fatal("IAM failure accepted")
	}
	if _, err := h.SupplierAccess(supplierIAM{})(ordinary, &mdv1.DeactivateSupplierRequest{Id: 1}, info, next); err == nil {
		t.Fatal("ordinary supplier deletion accepted")
	}
	highest := grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: 1, EmployeeID: 99})
	if _, err := h.SupplierAccess(supplierIAM{})(highest, &mdv1.DeactivateSupplierRequest{Id: 1}, info, next); err != nil {
		t.Fatalf("highest supplier deletion denied: %v", err)
	}
	if !called {
		t.Fatal("highest deletion did not reach handler")
	}
}

func TestSupplierAccessRestrictsOwnerManagement(t *testing.T) {
	h := New(nil)
	called := false
	next := func(context.Context, any) (any, error) {
		called = true
		return nil, nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/erp.masterdata.v1.SupplierService/CreateSupplierOwner"}
	req := &mdv1.CreateSupplierOwnerRequest{SupplierId: 12}
	ordinary := grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: 1, EmployeeID: 1})
	if _, err := h.SupplierAccess(supplierIAM{})(ordinary, req, info, next); err == nil {
		t.Fatal("ordinary user managed supplier owners")
	}
	if called {
		t.Fatal("denied owner mutation reached handler")
	}
	highest := grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: 1, EmployeeID: 99})
	if _, err := h.SupplierAccess(supplierIAM{})(highest, req, info, next); err != nil {
		t.Fatalf("highest owner management denied: %v", err)
	}
	if !called {
		t.Fatal("highest owner mutation did not reach handler")
	}
}

func TestSupplierAccessLifecycleIntegration(t *testing.T) {
	dsn := os.Getenv("MD_TEST_DSN")
	if dsn == "" {
		t.Skip("MD_TEST_DSN not set")
	}
	pool, err := pgdb.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	h := New(app.New(pool))
	tenant := time.Now().UnixNano() / 1000
	defer func() {
		for _, table := range []string{"supplier_change_logs", "supplier_contacts", "supplier_owners", "suppliers"} {
			_, _ = pool.Exec(context.Background(), "DELETE FROM "+table+" WHERE tenant_id=ANY($1::bigint[])", []int64{tenant, tenant + 1})
		}
	}()
	call := func(tid, actor int64, method string, req any) (any, error) {
		ctx := grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: tid, EmployeeID: actor, Name: "Employee"})
		return h.SupplierAccess(supplierIAM{})(ctx, req, &grpc.UnaryServerInfo{FullMethod: "/erp.masterdata.v1.SupplierService/" + method}, func(ctx context.Context, req any) (any, error) {
			values := reflect.ValueOf(h).MethodByName(method).Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(req)})
			if !values[1].IsNil() {
				return nil, values[1].Interface().(error)
			}
			return values[0].Interface(), nil
		})
	}
	must := func(tid, actor int64, method string, req any) any {
		t.Helper()
		result, err := call(tid, actor, method, req)
		if err != nil {
			t.Fatalf("%s actor %d: %v", method, actor, err)
		}
		return result
	}
	created := must(tenant, 1, "CreateSupplier", &mdv1.CreateSupplierRequest{Code: "OWNER-SCOPE", NameZh: "Owner scoped supplier", CountryCode: "CN", BusinessTypes: []string{"GENERAL"}}).(*mdv1.CreateSupplierResponse).Supplier
	id := created.Id
	owners := must(tenant, 1, "ListSupplierOwners", &mdv1.ListSupplierOwnersRequest{SupplierId: id}).(*mdv1.ListSupplierOwnersResponse).Owners
	if len(owners) != 1 || owners[0].EmployeeId != 1 || !owners[0].IsPrimary {
		t.Fatalf("creator not assigned: %v", owners)
	}
	list := func(actor int64) *mdv1.ListSuppliersResponse {
		return must(tenant, actor, "ListSuppliers", &mdv1.ListSuppliersRequest{Keyword: "Owner scoped", Page: &commonv1.PageRequest{Page: 1, PageSize: 20}}).(*mdv1.ListSuppliersResponse)
	}
	if r := list(1); len(r.Suppliers) != 1 {
		t.Fatal("creator cannot list supplier")
	}
	if r := list(2); len(r.Suppliers) != 0 {
		t.Fatal("unassigned user can list supplier")
	}
	imported := must(tenant, 1, "ImportSuppliers", &mdv1.ImportSuppliersRequest{Confirm: true, Rows: []*mdv1.SupplierImportRow{{RowNumber: 2, Code: "OWNER-IMPORT", NameZh: "Imported owner supplier", CountryCode: "CN", BusinessTypes: []string{"GENERAL"}}}}).(*mdv1.ImportSuppliersResponse)
	if imported.ImportedCount != 1 {
		t.Fatalf("supplier import failed: %v", imported)
	}
	importList := must(tenant, 1, "ListSuppliers", &mdv1.ListSuppliersRequest{Keyword: "Imported owner", Page: &commonv1.PageRequest{Page: 1, PageSize: 20}}).(*mdv1.ListSuppliersResponse)
	if len(importList.Suppliers) != 1 {
		t.Fatal("importer was not assigned to imported supplier")
	}
	if _, err := call(tenant, 2, "GetSupplier", &mdv1.GetSupplierRequest{Id: id}); err == nil {
		t.Fatal("unassigned user can read supplier")
	}
	if _, err := call(tenant, 1, "CreateSupplierOwner", &mdv1.CreateSupplierOwnerRequest{SupplierId: id, Owner: &mdv1.SupplierOwnerInput{EmployeeId: 2, EmployeeName: "B", ResponsibilityCode: "PROCUREMENT"}}); err == nil {
		t.Fatal("ordinary owner managed supplier owners")
	}
	ownerB := must(tenant, 99, "CreateSupplierOwner", &mdv1.CreateSupplierOwnerRequest{SupplierId: id, Owner: &mdv1.SupplierOwnerInput{EmployeeId: 2, EmployeeName: "B", ResponsibilityCode: "PROCUREMENT"}}).(*mdv1.CreateSupplierOwnerResponse).Owner
	must(tenant, 2, "GetSupplier", &mdv1.GetSupplierRequest{Id: id})
	must(tenant, 99, "DeactivateSupplierOwner", &mdv1.DeactivateSupplierOwnerRequest{SupplierId: id, Id: ownerB.Id})
	if _, err := call(tenant, 2, "GetSupplier", &mdv1.GetSupplierRequest{Id: id}); err == nil {
		t.Fatal("removed owner retained access")
	}
	if _, err := call(tenant+1, 99, "GetSupplier", &mdv1.GetSupplierRequest{Id: id}); err == nil {
		t.Fatal("cross-tenant supplier leaked")
	}
	if _, err := call(tenant, 1, "DeactivateSupplier", &mdv1.DeactivateSupplierRequest{Id: id, Reason: "test"}); err == nil {
		t.Fatal("ordinary owner deleted supplier")
	}
	must(tenant, 99, "DeactivateSupplier", &mdv1.DeactivateSupplierRequest{Id: id, Reason: "test"})
}

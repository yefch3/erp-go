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

type customerIAM struct {
	iamv1.AccessServiceClient
	failure error
}

func (s customerIAM) VisibleEmployees(ctx context.Context, req *iamv1.VisibleEmployeesRequest, _ ...grpc.CallOption) (*iamv1.VisibleEmployeesResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if req.Module != "customer" || req.EmployeeId != op.EmployeeID {
		return nil, errors.New("wrong scope identity")
	}
	return &iamv1.VisibleEmployeesResponse{All: op.EmployeeID == 99}, s.failure
}

func TestCustomerAccessFailsClosed(t *testing.T) {
	h := New(nil)
	called := false
	next := func(context.Context, any) (any, error) { called = true; return nil, nil }
	info := &grpc.UnaryServerInfo{FullMethod: "/erp.masterdata.v1.CustomerService/ListCustomers"}
	if _, err := h.CustomerAccess(customerIAM{})(context.Background(), &mdv1.ListCustomersRequest{}, info, next); err == nil {
		t.Fatal("missing identity accepted")
	}
	ctx := grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: 1, EmployeeID: 1})
	if _, err := h.CustomerAccess(customerIAM{failure: errors.New("IAM unavailable")})(ctx, &mdv1.ListCustomersRequest{}, info, next); err == nil {
		t.Fatal("IAM failure accepted")
	}
	info.FullMethod = "/erp.masterdata.v1.CustomerService/DeactivateCustomer"
	if _, err := h.CustomerAccess(customerIAM{})(ctx, &mdv1.DeactivateCustomerRequest{Id: 12}, info, next); err == nil {
		t.Fatal("ordinary owner deletion accepted")
	}
	if called {
		t.Fatal("denied call reached handler")
	}
}

func TestCustomerAccessLifecycleIntegration(t *testing.T) {
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
		for _, table := range []string{"customer_change_logs", "customer_contacts", "customer_addresses", "customer_owners", "customer_custom_field_values", "customers", "number_rules", "option_items"} {
			_, _ = pool.Exec(context.Background(), "DELETE FROM "+table+" WHERE tenant_id=ANY($1::bigint[])", []int64{tenant, tenant + 1})
		}
	}()
	call := func(tid, actor int64, method string, req any) (any, error) {
		ctx := grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: tid, EmployeeID: actor, Name: "Sales"})
		service := "CustomerService"
		if method == "ListCreditRatings" {
			service = "CreditRatingService"
		}
		return h.CustomerAccess(customerIAM{})(ctx, req, &grpc.UnaryServerInfo{FullMethod: "/erp.masterdata.v1." + service + "/" + method}, func(ctx context.Context, req any) (any, error) {
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
	created := must(tenant, 1, "CreateCustomer", &mdv1.CreateCustomerRequest{Name: "Scoped customer", CountryCode: "CN", Contacts: []*mdv1.Contact{{Name: "Contact", Email: "owner@example.com"}}}).(*mdv1.CreateCustomerResponse).Customer
	id := created.Id
	owners := must(tenant, 1, "ListCustomerOwners", &mdv1.ListCustomerOwnersRequest{CustomerId: id}).(*mdv1.ListCustomerOwnersResponse).Owners
	if len(owners) != 1 || owners[0].EmployeeId != 1 {
		t.Fatalf("creator not assigned: %v", owners)
	}
	list := func(actor int64, ownerFilter int64) *mdv1.ListCustomersResponse {
		return must(tenant, actor, "ListCustomers", &mdv1.ListCustomersRequest{Keyword: "Scoped", OwnerEmployeeId: ownerFilter, Page: &commonv1.PageRequest{Page: 1, PageSize: 1}}).(*mdv1.ListCustomersResponse)
	}
	if r := list(1, 0); len(r.Customers) != 1 || r.Meta.Total != 1 {
		t.Fatal("owner cannot list customer")
	}
	if r := list(4, 1); len(r.Customers) != 0 || r.Meta.Total != 0 {
		t.Fatal("owner filter bypassed scope")
	}
	if r := list(99, 0); len(r.Customers) != 1 {
		t.Fatal("admin cannot list customer")
	}
	for _, method := range []string{"GetCustomer", "UpdateCustomer", "UpdateCustomerProfile", "ListCustomerOwners", "CreateCustomerOwner", "ListCustomerContacts", "CreateCustomerContact", "ListCustomerAddresses", "ListCustomerChanges", "GetCustomerDeactivationImpact", "ListCreditRatings"} {
		requests := map[string]any{
			"GetCustomer":                   &mdv1.GetCustomerRequest{Id: id},
			"UpdateCustomer":                &mdv1.UpdateCustomerRequest{Id: id, Name: "Forbidden"},
			"UpdateCustomerProfile":         &mdv1.UpdateCustomerProfileRequest{Id: id},
			"ListCustomerOwners":            &mdv1.ListCustomerOwnersRequest{CustomerId: id},
			"CreateCustomerOwner":           &mdv1.CreateCustomerOwnerRequest{CustomerId: id, Owner: &mdv1.CustomerOwnerInput{EmployeeId: 4, EmployeeName: "D", ResponsibilityCode: "SALES"}},
			"ListCustomerContacts":          &mdv1.ListCustomerContactsRequest{CustomerId: id},
			"CreateCustomerContact":         &mdv1.CreateCustomerContactRequest{CustomerId: id},
			"ListCustomerAddresses":         &mdv1.ListCustomerAddressesRequest{CustomerId: id},
			"ListCustomerChanges":           &mdv1.ListCustomerChangesRequest{CustomerId: id},
			"GetCustomerDeactivationImpact": &mdv1.GetCustomerDeactivationImpactRequest{Id: id},
			"ListCreditRatings":             &mdv1.ListCreditRatingsRequest{PartyType: "CUSTOMER", PartyId: id},
		}
		if _, err := call(tenant, 4, method, requests[method]); err == nil {
			t.Fatalf("%s leaked", method)
		}
	}
	if r := must(tenant, 4, "CheckCustomerDuplicates", &mdv1.CheckCustomerDuplicatesRequest{Name: "Scoped customer"}).(*mdv1.CheckCustomerDuplicatesResponse); len(r.Candidates) != 0 {
		t.Fatal("duplicate search leaked")
	}
	if r := must(tenant, 4, "ListMailingContacts", &mdv1.ListMailingContactsRequest{CustomerIds: []int64{id}}).(*mdv1.ListMailingContactsResponse); len(r.Contacts) != 0 {
		t.Fatal("mail picker leaked")
	}
	if r := must(tenant, 4, "ListCustomerCountries", &mdv1.ListCustomerCountriesRequest{}).(*mdv1.ListCustomerCountriesResponse); len(r.Countries) != 0 {
		t.Fatal("country totals leaked")
	}
	if r := must(tenant, 4, "ContactsInCountry", &mdv1.ContactsInCountryRequest{CountryCode: "CN"}).(*mdv1.ContactsInCountryResponse); len(r.Contacts) != 0 {
		t.Fatal("country contacts leaked")
	}
	var ownerB int64
	for _, actor := range []int64{2, 3} {
		owner := must(tenant, 1, "CreateCustomerOwner", &mdv1.CreateCustomerOwnerRequest{CustomerId: id, Owner: &mdv1.CustomerOwnerInput{EmployeeId: actor, EmployeeName: "Added", ResponsibilityCode: "SALES"}}).(*mdv1.CreateCustomerOwnerResponse).Owner
		if actor == 2 {
			ownerB = owner.Id
		}
		must(tenant, actor, "GetCustomer", &mdv1.GetCustomerRequest{Id: id})
		must(tenant, actor, "UpdateCustomer", &mdv1.UpdateCustomerRequest{Id: id, Name: "Scoped customer", CountryCode: "CN"})
	}
	must(tenant, 99, "DeactivateCustomerOwner", &mdv1.DeactivateCustomerOwnerRequest{CustomerId: id, Id: ownerB})
	if _, err := call(tenant, 2, "GetCustomer", &mdv1.GetCustomerRequest{Id: id}); err == nil {
		t.Fatal("removed owner still has access")
	}
	if r := list(2, 0); len(r.Customers) != 0 {
		t.Fatal("removed owner can still search")
	}
	must(tenant, 1, "GetCustomer", &mdv1.GetCustomerRequest{Id: id})
	must(tenant, 3, "GetCustomer", &mdv1.GetCustomerRequest{Id: id})
	for _, actor := range []int64{1, 3, 4} {
		if _, err := call(tenant, actor, "DeactivateCustomer", &mdv1.DeactivateCustomerRequest{Id: id, Reason: "delete"}); err == nil {
			t.Fatal("non-admin deleted customer")
		}
	}
	// Same employee IDs in another company must never reuse this company's relation.
	for _, actor := range []int64{1, 99} {
		if _, err := call(tenant+1, actor, "GetCustomer", &mdv1.GetCustomerRequest{Id: id}); err == nil {
			t.Fatal("cross-tenant detail leaked")
		}
		r := must(tenant+1, actor, "ListCustomers", &mdv1.ListCustomersRequest{}).(*mdv1.ListCustomersResponse)
		if len(r.Customers) != 0 {
			t.Fatal("cross-tenant list leaked")
		}
		if _, err := call(tenant+1, actor, "DeactivateCustomer", &mdv1.DeactivateCustomerRequest{Id: id, Reason: "delete"}); err == nil {
			t.Fatal("cross-tenant delete succeeded")
		}
	}
	must(tenant, 99, "DeactivateCustomer", &mdv1.DeactivateCustomerRequest{Id: id, Reason: "delete"})
	if r := list(1, 0); len(r.Customers) != 0 {
		t.Fatal("deleted customer still selectable")
	}
}

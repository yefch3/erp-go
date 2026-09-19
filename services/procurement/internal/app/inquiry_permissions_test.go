package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

type inquiryManagerAccess struct{}

func (inquiryManagerAccess) VisibleEmployees(_ context.Context, id int64, _ string) (Visibility, error) {
	switch id {
	case 401, 601:
		return Visibility{EmployeeIDs: []int64{id, 101}}, nil
	case 501:
		return Visibility{All: true}, nil
	default:
		return Visibility{EmployeeIDs: []int64{id}}, nil
	}
}
func (inquiryManagerAccess) HasPermission(ctx context.Context, id int64, p string) (bool, error) {
	if id == 501 {
		return true, nil
	}
	if id == 401 {
		return p == "sales:inquiry:read" || p == "sales:inquiry:write" || p == "sales:inquiry:submit", nil
	}
	if id == 101 || id == 102 {
		return p == "sales:inquiry:read" || p == "sales:inquiry:write" || p == "sales:inquiry:submit", nil
	}
	if id == 601 {
		return p == "sales:inquiry:read", nil
	}
	return (d1Access{}).HasPermission(ctx, id, p)
}

func TestInquiryDeleteRequiresDedicatedPermission(t *testing.T) {
	s := &Service{scopes: inquiryManagerAccess{}}
	if err := s.inquiryDeleteAllowed(context.Background(), Operator{ID: 501}); err != nil {
		t.Fatalf("highest privilege account denied: %v", err)
	}
	for _, actor := range []int64{101, 401, 601} {
		if err := s.inquiryDeleteAllowed(context.Background(), Operator{ID: actor}); err == nil {
			t.Fatalf("actor %d deleted without dedicated permission", actor)
		}
	}
}

func TestInquiryWriteScope(t *testing.T) {
	s := &Service{scopes: inquiryManagerAccess{}}
	for _, tc := range []struct {
		name         string
		actor, owner int64
		allowed      bool
	}{
		{"self", 101, 101, true}, {"peer", 102, 101, false},
		{"manager self", 401, 401, true}, {"subordinate", 401, 101, true},
		{"outside management", 401, 102, false}, {"administrator", 501, 102, true},
		{"read scope without write", 601, 101, false}, {"department", 201, 101, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := s.authorizeInquiryOwner(context.Background(), Operator{ID: tc.actor}, tc.owner)
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v, err=%v", tc.allowed, err)
			}
		})
	}
}

func TestInquiryManagerLifecycle(t *testing.T) {
	dsn := os.Getenv("INQUIRY_PERMISSIONS_TEST_DSN")
	if dsn == "" {
		t.Skip("set INQUIRY_PERMISSIONS_TEST_DSN to an isolated local database")
	}
	if !strings.Contains(dsn, "@127.0.0.1:5433/inquiry_permissions_test?") {
		t.Fatal("isolated local database required")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	s := New(pool, Deps{Scopes: inquiryManagerAccess{}, InquiryDocuments: d1Documents{}})
	call := func(actor int64, command InquiryCommand) *InquiryView {
		t.Helper()
		r, e := s.InquiryWorkspace(ctx, tenant, Operator{ID: actor, Name: "permission test"}, command)
		if e != nil {
			t.Fatalf("actor=%d action=%s: %v", actor, command.Action, e)
		}
		return r.Item
	}
	v := call(101, InquiryCommand{Action: "save", View: "SALES", Body: InquiryBody{Customer: "Permission test", Products: []InquiryProduct{{Product: "Steel", Quantity: "3", Unit: "MT"}}}})
	for _, actor := range []int64{102, 601, 201} {
		_, e := s.InquiryWorkspace(ctx, tenant, Operator{ID: actor}, InquiryCommand{Action: "save", View: "SALES", ID: v.ID, Revision: v.Revision, Body: v.Body})
		if e == nil {
			t.Fatalf("unauthorized write by %d", actor)
		}
	}
	ro := call(601, InquiryCommand{Action: "get", View: "SALES", ID: v.ID})
	if ro.CanEdit || ro.ReadOnlyReason == "" {
		t.Fatal("read-only capabilities incorrect")
	}
	v = call(401, InquiryCommand{Action: "save", View: "SALES", ID: v.ID, Revision: v.Revision, Body: v.Body})
	if !v.CanEdit || v.OwnerID != "101" {
		t.Fatal("manager edit changed ownership or lacks capability")
	}
	v = call(401, InquiryCommand{Action: "submit", View: "SALES", ID: v.ID, Revision: v.Revision})
	if !v.CanWithdraw {
		t.Fatal("manager cannot withdraw")
	}
	oldRevision := v.Revision
	v = call(501, InquiryCommand{Action: "withdraw", View: "SALES", ID: v.ID, Revision: v.Revision})
	if v.State != "WITHDRAWN" || v.OwnerID != "101" {
		t.Fatal("withdraw changed owner or wrong state")
	}
	_, err = s.InquiryWorkspace(ctx, tenant, Operator{ID: 501}, InquiryCommand{Action: "submit", View: "SALES", ID: v.ID, Revision: oldRevision})
	if err == nil {
		t.Fatal("stale revision accepted")
	}
	v = call(401, InquiryCommand{Action: "submit", View: "SALES", ID: v.ID, Revision: v.Revision})
	var recorded int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM sourcing_case_changes WHERE tenant_id=$1 AND case_id=$2 AND action='WITHDRAW' AND operator_id=501`, tenant, inquiryID(v.ID)).Scan(&recorded)
	if err != nil || recorded != 1 {
		t.Fatalf("actual operator not recorded: %d %v", recorded, err)
	}
	_, err = s.InquiryWorkspace(ctx, tenant+1, Operator{ID: 501}, InquiryCommand{Action: "withdraw", View: "SALES", ID: v.ID, Revision: v.Revision})
	if err == nil {
		t.Fatal("cross-tenant administrator write allowed")
	}
	// Build a real confirmed-customer chain; even an administrator must respect it.
	var procurementPlan, salesPlan int64
	err = pool.QueryRow(ctx, `INSERT INTO procurement_plans(tenant_id,case_id,plan_no,version_no,requirement_version_no,created_by,confirmed_by,target_sales_id) VALUES($1,$2,'PP-TEST',1,1,201,201,101) RETURNING id`, tenant, inquiryID(v.ID)).Scan(&procurementPlan)
	if err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `INSERT INTO sourcing_sales_plans(tenant_id,case_id,plan_no,version_no,requirement_version_no,procurement_plan_id,valid_until,created_by) VALUES($1,$2,'SP-TEST',1,1,$3,current_date,101) RETURNING id`, tenant, inquiryID(v.ID), procurementPlan).Scan(&salesPlan)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO sourcing_customer_selections(tenant_id,case_id,sales_plan_id,selection_no,version_no,requirement_version_no,status,customer_confirmed_at,created_by) VALUES($1,$2,$3,'CS-TEST',1,1,'CUSTOMER_CONFIRMED',now(),101)`, tenant, inquiryID(v.ID), salesPlan)
	if err != nil {
		t.Fatal(err)
	}
	v = call(501, InquiryCommand{Action: "get", View: "SALES", ID: v.ID})
	if v.CanWithdraw || v.WithdrawReason == "" {
		t.Fatal("confirmed inquiry exposes withdrawal")
	}
	_, err = s.InquiryWorkspace(ctx, tenant, Operator{ID: 501}, InquiryCommand{Action: "withdraw", View: "SALES", ID: v.ID, Revision: v.Revision})
	if err == nil {
		t.Fatal("administrator withdrew confirmed inquiry")
	}
	// Seed the independent procurement and shipping work queues. Deleting the
	// inquiry must close these rows too, otherwise the same business document
	// remains actionable outside the shared inquiry workspace.
	_, err = pool.Exec(ctx, `INSERT INTO factory_rfqs(tenant_id,case_id,rfq_no,supplier_id,supplier_name,status,created_by,created_by_name)
		VALUES($1,$2,'RFQ-DELETE-TEST',9001,'Delete test factory','SENT',201,'buyer')`, tenant, inquiryID(v.ID))
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO procurement_rework_requests(tenant_id,case_id,request_type,scope_type,reason,created_by,created_by_name)
		VALUES($1,$2,'REQUOTE','ALL','delete cascade test',101,'sales')`, tenant, inquiryID(v.ID))
	if err != nil {
		t.Fatal(err)
	}
	var shippingRequestID int64
	err = pool.QueryRow(ctx, `INSERT INTO sourcing_shipping_requests(tenant_id,case_id,status,case_no,sales_employee_id,requested_by)
		VALUES($1,$2,'QUOTING','DELETE-TEST',101,101)
		ON CONFLICT(tenant_id,case_id) DO UPDATE SET status='QUOTING',updated_at=now()
		RETURNING id`, tenant, inquiryID(v.ID)).Scan(&shippingRequestID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO sourcing_shipping_options(tenant_id,request_id,carrier_forwarder,status,created_by)
		VALUES($1,$2,'Delete test carrier','SUBMITTED',301)`, tenant, shippingRequestID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO sourcing_shipping_rework_requests(tenant_id,case_id,request_type,scope_type,reason,created_by,created_by_name)
		VALUES($1,$2,'REQUOTE','ALL','delete cascade test',101,'sales')`, tenant, inquiryID(v.ID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.InquiryWorkspace(ctx, tenant, Operator{ID: 401}, InquiryCommand{Action: "delete", View: "SALES", ID: v.ID}); err == nil {
		t.Fatal("sales manager deleted inquiry without dedicated permission")
	}
	if _, err = s.InquiryWorkspace(ctx, tenant, Operator{ID: 501, Name: "permission admin"}, InquiryCommand{Action: "delete", View: "SALES", ID: v.ID}); err != nil {
		t.Fatalf("privileged delete: %v", err)
	}
	var deletedBy int64
	var deletedAt time.Time
	if err = pool.QueryRow(ctx, `SELECT deleted_by_id,deleted_at FROM sourcing_cases WHERE tenant_id=$1 AND id=$2`, tenant, inquiryID(v.ID)).Scan(&deletedBy, &deletedAt); err != nil || deletedBy != 501 || deletedAt.IsZero() {
		t.Fatalf("delete audit not retained: by=%d at=%v err=%v", deletedBy, deletedAt, err)
	}
	for table, query := range map[string]string{
		"factory RFQ":        `SELECT count(*) FROM factory_rfqs WHERE tenant_id=$1 AND case_id=$2 AND status<>'CANCELLED'`,
		"procurement rework": `SELECT count(*) FROM procurement_rework_requests WHERE tenant_id=$1 AND case_id=$2 AND status<>'CANCELLED'`,
		"shipping request":   `SELECT count(*) FROM sourcing_shipping_requests WHERE tenant_id=$1 AND case_id=$2 AND status<>'CANCELLED'`,
		"shipping option":    `SELECT count(*) FROM sourcing_shipping_options WHERE tenant_id=$1 AND request_id=$2 AND status<>'CANCELLED'`,
		"shipping rework":    `SELECT count(*) FROM sourcing_shipping_rework_requests WHERE tenant_id=$1 AND case_id=$2 AND status<>'CANCELLED'`,
	} {
		foreignID := inquiryID(v.ID)
		if table == "shipping option" {
			foreignID = shippingRequestID
		}
		if err = pool.QueryRow(ctx, query, tenant, foreignID).Scan(&recorded); err != nil || recorded != 0 {
			t.Fatalf("%s remained active after delete: count=%d err=%v", table, recorded, err)
		}
	}
	if _, err = s.InquiryWorkspace(ctx, tenant, Operator{ID: 501}, InquiryCommand{Action: "get", View: "SALES", ID: v.ID}); err == nil {
		t.Fatal("soft-deleted inquiry remained visible")
	}
	for _, view := range []string{"SALES", "PROCUREMENT", "LOGISTICS"} {
		result, listErr := s.InquiryWorkspace(ctx, tenant, Operator{ID: 501}, InquiryCommand{Action: "list", View: view, Page: 1, Size: 20})
		if listErr != nil {
			t.Fatalf("list deleted inquiry in %s: %v", view, listErr)
		}
		if result.Total != 0 || len(result.Items) != 0 {
			t.Fatalf("deleted inquiry remained in %s: total=%d items=%d", view, result.Total, len(result.Items))
		}
		if _, getErr := s.InquiryWorkspace(ctx, tenant, Operator{ID: 501}, InquiryCommand{Action: "get", View: view, ID: v.ID}); getErr == nil {
			t.Fatalf("deleted inquiry detail remained accessible in %s", view)
		}
	}
	if _, legacyErr := s.GetSourcingCase(ctx, tenant, inquiryID(v.ID)); legacyErr == nil {
		t.Fatal("deleted inquiry remained accessible through legacy sourcing detail")
	}
}

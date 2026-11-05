package app

import (
	"context"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestTeamQuotationSnapshotWithoutCustomerOwnership(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN required")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	defer func() { _, _ = pool.Exec(ctx, "DELETE FROM sourcing_cases WHERE tenant_id=$1", tenant) }()
	var id int64
	err = pool.QueryRow(ctx, `INSERT INTO sourcing_cases(tenant_id,case_no,owner_id,owner_name,status,handoff_status,customer_id,customer_name,inquiry_body)
	VALUES($1,'TEAM-QUOTE',101,'Sales','REVIEWING','WAITING_ACCEPTANCE',10,'Recorded Customer','{"customerId":"10","customer":"Recorded Customer","products":[{"id":"a","product":"Steel","quantity":"50","unit":"MT"}]}') RETURNING id`, tenant).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool, Deps{Customers: &inquiryCustomerFixture{}, Scopes: inquiryManagerAccess{}})
	manager := Operator{ID: 401}
	get := InquiryCommand{Action: "get", View: "QUOTATIONS", ID: strconv.FormatInt(id, 10)}
	result, err := svc.InquiryWorkspace(ctx, tenant, manager, get)
	if err != nil || result.Item == nil || result.Item.Body.Customer != "Recorded Customer" {
		t.Fatalf("team snapshot: %v %+v", err, result.Item)
	}
	list, err := svc.InquiryWorkspace(ctx, tenant, manager, InquiryCommand{Action: "list", View: "QUOTATIONS"})
	if err != nil || list.Total != 1 {
		t.Fatalf("team quotation list: %v total=%d", err, list.Total)
	}
	for _, check := range []struct{ tenant, actor int64 }{{tenant, 102}, {tenant + 1, 401}, {tenant, 101}} {
		if _, err := svc.InquiryWorkspace(ctx, check.tenant, Operator{ID: check.actor}, get); err == nil {
			t.Fatalf("unrelated tenant/employee or former owner allowed: %+v", check)
		}
	}
	// SALES editing and customer master ownership retain their existing gates.
	get.View = "SALES"
	if _, err := svc.InquiryWorkspace(ctx, tenant, manager, get); err == nil {
		t.Fatal("quotation snapshot access widened inquiry/customer access")
	}
}

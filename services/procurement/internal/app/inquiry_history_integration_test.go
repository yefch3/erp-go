package app

import (
	"context"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"os"
	"strings"
	"testing"
	"time"
)

func TestD1HistoricalProjection(t *testing.T) {
	dsn := os.Getenv("D1_PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("run with an explicit D1 isolated DSN; not part of the default unit run")
	}
	if os.Getenv("D1_CONTAINER_NETWORK") != "erp-d1-20260906_isolated" || !strings.Contains(dsn, "@postgres:5432/erp_procurement?") {
		t.Fatal("D1 isolated DSN required")
	}
	ctx := context.Background()
	pool, e := pgdb.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	svc := New(pool, Deps{Scopes: d1Access{}, InquiryDocuments: d1Documents{}})
	var id int64
	e = pool.QueryRow(ctx, `INSERT INTO sourcing_cases(tenant_id,case_no,owner_id,owner_name,status,handoff_status) VALUES($1,'D1-history',101,'sales','REVIEWING','IN_PROGRESS') RETURNING id`, tenant).Scan(&id)
	if e != nil {
		t.Fatal(e)
	}
	var line int64
	e = pool.QueryRow(ctx, `INSERT INTO sourcing_lines(tenant_id,case_id,line_no,product,quantity,quantity_unit) VALUES($1,$2,1,'Historical steel',3,'MT') RETURNING id`, tenant, id).Scan(&line)
	if e != nil {
		t.Fatal(e)
	}
	_, e = pool.Exec(ctx, `WITH r AS (INSERT INTO factory_rfqs(tenant_id,case_id,rfq_no,supplier_id,supplier_name) VALUES($1,$2,'D1-H-R',1,'History factory') RETURNING id),
 q AS(INSERT INTO supplier_quotes(tenant_id,factory_rfq_id,supplier_quote_no,currency) SELECT $1,id,'D1-H-Q','USD' FROM r RETURNING id),
 l AS(INSERT INTO supplier_quote_lines(tenant_id,supplier_quote_id,sourcing_line_id,qty,unit_price,amount) SELECT $1,id,$3,3,10,30 FROM q RETURNING id),
 p AS(INSERT INTO procurement_plans(tenant_id,case_id,plan_no,version_no,requirement_version_no,created_by,confirmed_by,target_sales_id) VALUES($1,$2,'D1-H-P',1,1,201,201,101) RETURNING id)
 INSERT INTO procurement_plan_items(tenant_id,plan_id,sourcing_line_id,supplier_quote_line_id,selection_type,priority,supplier_id,supplier_name,buyer_id,buyer_name,product_name,currency,unit_price,available_qty,uom_code,quote_version_no)
 SELECT $1,p.id,$3,l.id,'RECOMMENDED',1,1,'History factory',201,'Original buyer','Historical steel','USD',10,3,'MT',1 FROM p,l`, tenant, id, line)
	if e != nil {
		t.Fatal(e)
	}
	history, e := svc.inquiryHistory(ctx, tenant, id, "QUOTATIONS")
	if e != nil || len(history) != 0 {
		t.Fatalf("unsubmitted historical plan leaked: %v %v", history, e)
	}
	_, e = pool.Exec(ctx, `UPDATE procurement_plans SET submitted_to_sales_at=now(),status='SUBMITTED_TO_SALES' WHERE tenant_id=$1 AND case_id=$2`, tenant, id)
	if e != nil {
		t.Fatal(e)
	}
	_, e = pool.Exec(ctx, `UPDATE supplier_quote_lines SET unit_price=999 WHERE tenant_id=$1`, tenant)
	if e != nil {
		t.Fatal(e)
	}
	history, e = svc.inquiryHistory(ctx, tenant, id, "QUOTATIONS")
	if e != nil || len(history) != 1 {
		t.Fatalf("historical projection: %v %v", history, e)
	}
	if !history[0].Historical || history[0].CanEdit || history[0].Body.Prices[0].Price != "10.000000" {
		t.Fatalf("historical snapshot changed: %+v", history[0])
	}
	other, e := svc.inquiryHistory(ctx, tenant+1, id, "QUOTATIONS")
	if e != nil || len(other) != 0 {
		t.Fatal("historical tenant leak", e)
	}
	var count int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM inquiry_quotes WHERE tenant_id=$1`, tenant).Scan(&count); e != nil || count != 0 {
		t.Fatal("historical records were converted", e)
	}
	// Keep the original records and identifiers. A new quote can coexist without
	// changing the old plan or forcing an invented migration/approval gate.
	got, e := svc.readInquiry(ctx, tenant, id, Operator{ID: 101}, "QUOTATIONS")
	if e != nil || !got.Legacy || len(got.Quotes) != 1 {
		t.Fatal("legacy inquiry read", e)
	}
	_, e = pool.Exec(ctx, `WITH r AS(INSERT INTO sourcing_shipping_requests(tenant_id,case_id,case_no,sales_employee_id,requested_by) VALUES($1,$2,'D1-H',101,101) RETURNING id),
 o AS(INSERT INTO sourcing_shipping_options(tenant_id,request_id,carrier_forwarder,created_by,status) SELECT $1,id,'Historical carrier/forwarder',301,'SUBMITTED' FROM r RETURNING id,request_id),
 l AS(INSERT INTO sourcing_shipping_option_lines(tenant_id,option_id,sourcing_line_id,line_no,currency,charge_basis,unit_rate,total_freight) SELECT $1,id,$3,1,'CNY','FIXED',21.11,42.22 FROM o RETURNING id),
 p AS(INSERT INTO sourcing_shipping_plans(tenant_id,request_id,plan_no,version_no,requirement_version_no,created_by,target_sales_id,submitted_to_sales_at) SELECT $1,request_id,'D1-H-SP',1,1,301,101,now() FROM o RETURNING id)
 INSERT INTO sourcing_shipping_plan_items(tenant_id,plan_id,sourcing_line_id,shipping_option_line_id,selection_type,priority,carrier_forwarder,shipping_employee_id,product_name,currency,charge_basis,unit_rate,total_freight,quote_version_no)
 SELECT $1,p.id,$3,l.id,'RECOMMENDED',1,'Historical carrier/forwarder',301,'Historical steel','CNY','FIXED',21.11,42.22,1 FROM p,l`, tenant, id, line)
	if e != nil {
		t.Fatal(e)
	}
	history, e = svc.inquiryHistory(ctx, tenant, id, "LOGISTICS")
	if e != nil || len(history) != 1 || history[0].Body.Charges[0].Subtotal != "42.220000" || !history[0].Historical {
		t.Fatal("shipping history snapshot", e)
	}
	var selectionID int64
	e = pool.QueryRow(ctx, `WITH p AS(INSERT INTO sourcing_sales_plans(tenant_id,case_id,plan_no,version_no,requirement_version_no,procurement_plan_id,valid_until,created_by) SELECT $1,$2,'D1-H-sales',1,1,id,current_date+7,101 FROM procurement_plans WHERE tenant_id=$1 AND case_id=$2 RETURNING id)
 INSERT INTO sourcing_customer_selections(tenant_id,case_id,sales_plan_id,selection_no,version_no,requirement_version_no,status,customer_confirmed_at,created_by) SELECT $1,$2,id,'D1-H-selection',1,1,'AWAITING_CUSTOMER_CONFIRMATION',now(),101 FROM p RETURNING id`, tenant, id).Scan(&selectionID)
	if e != nil {
		t.Fatal(e)
	}
	// Confirmation holds the source lock. A withdrawal starting concurrently
	// must wait and then reject the confirmed selection without deleting it.
	tx, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `UPDATE sourcing_customer_selections SET status='CUSTOMER_CONFIRMED' WHERE tenant_id=$1 AND id=$2`, tenant, selectionID); e != nil {
		t.Fatal(e)
	}
	result := make(chan error, 1)
	go func() {
		_, err := svc.InquiryWorkspace(ctx, tenant, Operator{ID: 101}, InquiryCommand{Action: "withdraw", View: "SALES", ID: got.ID, Revision: got.Revision})
		result <- err
	}()
	select {
	case err := <-result:
		t.Fatal("withdrawal did not wait for local confirmation", err)
	case <-time.After(100 * time.Millisecond):
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("confirmed history withdrawn")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("withdrawal hung")
	}
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM sourcing_customer_selections WHERE tenant_id=$1 AND id=$2 AND status='CUSTOMER_CONFIRMED'`, tenant, selectionID).Scan(&count); e != nil || count != 1 {
		t.Fatal("confirmed history lost", e)
	}
	// The other ordering: an already withdrawn source rejects a stale confirm.
	if _, e = pool.Exec(ctx, `UPDATE sourcing_cases SET status='INTAKE_PENDING',handoff_status='SALES_WITHDRAWN' WHERE tenant_id=$1 AND id=$2`, tenant, id); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `UPDATE sourcing_customer_selections SET status='CUSTOMER_CONFIRMED' WHERE tenant_id=$1 AND id=$2`, tenant, selectionID); e == nil {
		t.Fatal("stale local confirmation accepted")
	}
	t.Logf("tenant=%d historical case=%d: unsubmitted hidden; submitted immutable plan price retained; no new quote rows; tenant isolated", tenant, id)
}

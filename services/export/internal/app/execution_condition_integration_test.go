package app

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestFinanceExecutionConditionReleasesContractOnce(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		for _, table := range []string{"outbox_events", "contract_items", "contract_versions", "contracts"} {
			_, _ = pool.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1`, tenantID)
		}
	}()

	var contractID, versionID int64
	err = pool.QueryRow(ctx, `INSERT INTO contracts
		(tenant_id,contract_no,customer_id,customer_name,status,sales_employee_id,sales_employee,created_by,updated_by)
		VALUES ($1,$2,81,'D3 Customer','EXECUTING',91,'D3 Sales',91,91) RETURNING id`,
		tenantID, "CT-D3-"+time.Now().Format("150405.000000")).Scan(&contractID)
	if err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `INSERT INTO contract_versions
		(tenant_id,contract_id,version_no,status,currency,total_amount,fx_rate,fx_rate_at,fx_source,created_by)
		VALUES ($1,$2,1,'DRAFT','USD',1250,1,now(),'TEST',91) RETURNING id`, tenantID, contractID).Scan(&versionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE contracts SET current_version_id=$2 WHERE tenant_id=$1 AND id=$3`, tenantID, versionID, contractID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO contract_items
		(tenant_id,contract_version_id,line_no,product_id,product_code,product_name,spec,qty,uom_id,uom_code,unit_price,amount)
		VALUES ($1,$2,1,101,'P-101','D3 Coil','1.2x1250',10,1,'MT',125,1250)`, tenantID, versionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE contract_versions SET status='APPROVED' WHERE tenant_id=$1 AND id=$2`, tenantID, versionID); err != nil {
		t.Fatal(err)
	}

	svc := New(pool, Deps{})
	if _, err = svc.ConfirmContractExecutionCondition(ctx, tenantID, contractID, "BAD", "", Operator{ID: 1, Name: "Finance"}); err == nil {
		t.Fatal("invalid condition accepted")
	}
	got, err := svc.ConfirmContractExecutionCondition(ctx, tenantID, contractID, ConditionPrepaymentReceived, "bank advice checked", Operator{ID: 71, Name: "D3 Finance"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "READY" || got.ConditionType != ConditionPrepaymentReceived || got.ConfirmedAt == "" {
		t.Fatalf("unexpected result: %+v", got)
	}
	// A retry returns the first decision and must not publish another release.
	retry, err := svc.ConfirmContractExecutionCondition(ctx, tenantID, contractID, ConditionSpecialApproval, "changed", Operator{ID: 72, Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	if retry.ConditionType != ConditionPrepaymentReceived || retry.ConfirmedByName != "D3 Finance" {
		t.Fatalf("retry rewrote decision: %+v", retry)
	}
	var events int
	contractKey := strconv.FormatInt(contractID, 10)
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='ContractEffective'`, tenantID, contractKey).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("release events=%d, want 1", events)
	}
	var raw []byte
	if err = pool.QueryRow(ctx, `SELECT payload FROM outbox_events WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='ContractEffective'`, tenantID, contractKey).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var event contractEffectiveEvent
	if err = json.Unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	if len(event.Shipments) != 1 || event.Shipments[0].CarrierForwarder != "" {
		t.Fatalf("D3 fallback shipping task must exist without locking a forwarder: %+v", event.Shipments)
	}
}

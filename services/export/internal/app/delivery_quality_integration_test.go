package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/shopspring/decimal"
)

type sailingQualityStub struct {
	mu         sync.Mutex
	err        error
	limit      string
	contractID int64
	lines      []DeliveryQualityLine
}

func (g *sailingQualityStub) Check(_ context.Context, contractID int64, lines []DeliveryQualityLine) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.contractID, g.lines = contractID, append([]DeliveryQualityLine(nil), lines...)
	if g.err != nil {
		return g.err
	}
	for _, line := range lines {
		if g.limit != "" && decimal.RequireFromString(line.Qty).GreaterThan(decimal.RequireFromString(g.limit)) {
			return apierr.Conflict("QUALITY_QTY_NOT_PASSED", "已合格数量不足以覆盖累计交货")
		}
	}
	return nil
}

func sailingQualityDB(t *testing.T) (context.Context, *pgxpool.Pool, int64, int64, int64, []int64) {
	t.Helper()
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	tenant := time.Now().UnixNano()
	t.Cleanup(func() {
		for _, table := range []string{"shipment_items", "shipments", "contract_shipments", "contract_items", "contract_versions", "contracts"} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenant)
		}
		pool.Close()
	})
	var contract, version, item int64
	if err := pool.QueryRow(ctx, `INSERT INTO contracts (tenant_id,contract_no,customer_id,customer_name,status,sales_employee_id,sales_employee,created_by,updated_by) VALUES ($1,'CT-QUALITY',1,'客户','EFFECTIVE',1,'销售',1,1) RETURNING id`, tenant).Scan(&contract); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO contract_versions (tenant_id,contract_id,version_no,status,currency,total_amount,created_by,fx_rate,fx_rate_at,fx_source) VALUES ($1,$2,1,'DRAFT','USD',1000,1,1,now(),'TEST') RETURNING id`, tenant, contract).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO contract_items (tenant_id,contract_version_id,line_no,product_id,product_name,qty,uom_code,unit_price,amount) VALUES ($1,$2,1,1,'钢材',10,'MT',100,1000) RETURNING id`, tenant, version).Scan(&item); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE contract_versions SET status='APPROVED' WHERE tenant_id=$1 AND id=$2`, tenant, version); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE contracts SET current_version_id=$3 WHERE tenant_id=$1 AND id=$2`, tenant, contract, version); err != nil {
		t.Fatal(err)
	}
	var shipments []int64
	for i := 0; i < 2; i++ {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO shipments (tenant_id,shipment_no,created_by) VALUES ($1,$2,1) RETURNING id`, tenant, fmt.Sprintf("SH-QUALITY-%d", i)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO shipment_items (tenant_id,shipment_id,line_no,contract_id,contract_no,contract_item_id,product_id,product_name,qty,uom_code) VALUES ($1,$2,1,$3,'CT-QUALITY',$4,1,'钢材',5,'MT')`, tenant, id, contract, item); err != nil {
			t.Fatal(err)
		}
		shipments = append(shipments, id)
	}
	return ctx, pool, tenant, contract, item, shipments
}

func TestLegacySailingRequiresPassedQualityAndRollsBackBlockedShipment(t *testing.T) {
	ctx, pool, tenant, contract, item, shipments := sailingQualityDB(t)
	svc := New(pool, Deps{})
	op := Operator{ID: 1, Name: "销售"}
	if _, err := svc.ConfirmSailing(ctx, tenant, shipments[0], op); err == nil {
		t.Fatal("missing quality checker allowed sailing")
	}
	for _, state := range []string{"未质检", "质检中", "不合格", "复检中", "服务不可用"} {
		t.Run(state, func(t *testing.T) {
			svc.UseDeliveryQualityGuard(&sailingQualityStub{err: errors.New(state)})
			if _, err := svc.ConfirmSailing(ctx, tenant, shipments[0], op); err == nil {
				t.Fatal("blocked quality allowed sailing")
			}
			var status string
			var count int
			if err := pool.QueryRow(ctx, `SELECT status FROM shipments WHERE tenant_id=$1 AND id=$2`, tenant, shipments[0]).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM contract_shipments WHERE tenant_id=$1`, tenant).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if status != "DRAFT" || count != 0 {
				t.Fatalf("blocked sailing changed state: %s, ledger=%d", status, count)
			}
		})
	}
	guard := &sailingQualityStub{}
	svc.UseDeliveryQualityGuard(guard)
	if _, err := svc.ConfirmSailing(ctx, tenant, shipments[0], op); err != nil {
		t.Fatal(err)
	}
	if guard.contractID != contract || len(guard.lines) != 1 || guard.lines[0].ContractItemID != item || guard.lines[0].Qty != "5" {
		t.Fatalf("wrong quality target: %+v", guard)
	}
	if _, err := svc.ConfirmSailing(ctx, tenant, shipments[1], op); err != nil {
		t.Fatal(err)
	}
	if guard.lines[0].Qty != "10" {
		t.Fatalf("previous batch not included: %+v", guard.lines)
	}
}

func TestLegacySailingSerializesInspectionCoverageAcrossBatches(t *testing.T) {
	ctx, pool, tenant, _, _, shipments := sailingQualityDB(t)
	svc := New(pool, Deps{})
	svc.UseDeliveryQualityGuard(&sailingQualityStub{limit: "5"})
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, id := range shipments {
		go func(id int64) {
			<-start
			_, err := svc.ConfirmSailing(ctx, tenant, id, Operator{ID: 1, Name: "销售"})
			results <- err
		}(id)
	}
	close(start)
	passed, blocked := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			passed++
			continue
		}
		var api *apierr.Error
		if errors.As(err, &api) && api.Code == "QUALITY_QTY_NOT_PASSED" {
			blocked++
		} else {
			t.Fatal(err)
		}
	}
	if passed != 1 || blocked != 1 {
		t.Fatalf("passed quantity reused: passed=%d blocked=%d", passed, blocked)
	}
}

func TestLegacySailingCarriesInspectionUsageAcrossContractVersions(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		t.Run(fmt.Sprintf("ambiguous=%v", ambiguous), func(t *testing.T) {
			ctx, pool, tenant, contract, oldItem, shipments := sailingQualityDB(t)
			svc := New(pool, Deps{})
			guard := &sailingQualityStub{limit: "5"}
			svc.UseDeliveryQualityGuard(guard)
			op := Operator{ID: 1, Name: "销售"}
			if _, err := svc.ConfirmSailing(ctx, tenant, shipments[0], op); err != nil {
				t.Fatal(err)
			}
			var version, item int64
			if err := pool.QueryRow(ctx, `INSERT INTO contract_versions (tenant_id,contract_id,version_no,status,currency,total_amount,created_by,fx_rate,fx_rate_at,fx_source) VALUES ($1,$2,2,'DRAFT','USD',1000,1,1,now(),'TEST') RETURNING id`, tenant, contract).Scan(&version); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `INSERT INTO contract_items (tenant_id,contract_version_id,line_no,product_id,product_name,qty,uom_code,unit_price,amount) SELECT tenant_id,$3,1,product_id,product_name,10,uom_code,100,1000 FROM contract_items WHERE tenant_id=$1 AND id=$2 RETURNING id`, tenant, oldItem, version).Scan(&item); err != nil {
				t.Fatal(err)
			}
			if ambiguous {
				if _, err := pool.Exec(ctx, `INSERT INTO contract_items (tenant_id,contract_version_id,line_no,product_id,product_name,qty,uom_code,unit_price,amount) SELECT tenant_id,$3,2,product_id,product_name,10,uom_code,100,1000 FROM contract_items WHERE tenant_id=$1 AND id=$2`, tenant, oldItem, version); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := pool.Exec(ctx, `UPDATE contract_versions SET status='APPROVED' WHERE tenant_id=$1 AND id=$2`, tenant, version); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE contracts SET current_version_id=$3 WHERE tenant_id=$1 AND id=$2`, tenant, contract, version); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE shipment_items SET contract_item_id=$3 WHERE tenant_id=$1 AND shipment_id=$2`, tenant, shipments[1], item); err != nil {
				t.Fatal(err)
			}
			_, err := svc.ConfirmSailing(ctx, tenant, shipments[1], op)
			var api *apierr.Error
			want := "QUALITY_QTY_NOT_PASSED"
			if ambiguous {
				want = "QUALITY_VERSION_ALLOCATION_REQUIRED"
			}
			if !errors.As(err, &api) || api.Code != want {
				t.Fatalf("cross-version gate = %v, want %s", err, want)
			}
			if !ambiguous {
				if len(guard.lines) != 1 || guard.lines[0].ContractItemID != item || guard.lines[0].Qty != "10" {
					t.Fatalf("old version quantity lost: %+v", guard.lines)
				}
				guard.limit = "10"
				if _, err := svc.ConfirmSailing(ctx, tenant, shipments[1], op); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

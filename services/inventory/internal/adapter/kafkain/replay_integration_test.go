package kafkain

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/deadletter"
	"github.com/sgao19/erp-go/pkg/kafkax"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/inventory/internal/app"
)

// 死信重放的整条链，用真实事故的形状走一遍：
//
// 一笔采购收货因为「这家公司没有仓库」处理失败、被停进死信表——采购那边
// 显示已收货，库存一动不动。管理员建好仓库之后，在平台页点「重放」：事件
// 走的是当初失败的同一条处理路，这次成功，库存补记，死信离场。
//
// 修不好原因就点重放的，拿到的是原因本身，事件原地不动——重放不是重试按钮，
// 是「原因已修好」的宣告。
func TestReplayHealsAReceiptThatWasDroppedForWantOfAWarehouse(t *testing.T) {
	dsn := os.Getenv("INVENTORY_TEST_DSN")
	if dsn == "" {
		t.Skip("INVENTORY_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := app.New(pool, nil)
	tenantID := time.Now().UnixNano()
	group := fmt.Sprintf("test-replay-%d", tenantID)
	t.Cleanup(func() {
		for _, stmt := range []string{
			"DELETE FROM stock_ledger WHERE tenant_id=$1",
			"DELETE FROM stocks WHERE tenant_id=$1",
			"DELETE FROM warehouses WHERE tenant_id=$1",
			"DELETE FROM failed_events WHERE tenant_id=$1",
		} {
			if _, err := pool.Exec(context.Background(), stmt, tenantID); err != nil {
				t.Errorf("cleanup %q: %v", stmt, err)
			}
		}
	})

	// 事故现场：一笔没有指定仓库的收货，而这家公司一个仓库都没有。
	payload, _ := json.Marshal(app.PurchaseReceived{
		POID: 42, PONo: "PO-TEST", ReceiptNo: "R-TEST", OperatorID: 7,
		Lines: []app.PurchaseLine{{
			ProductID: 1, ProductCode: "P1", ProductName: "测试品",
			UomID: 1, UomCode: "PCS", Qty: "10", UnitCost: "3",
		}},
	})
	store := deadletter.New(pool, group)
	if err := store.Park(ctx, tenantID, "purchase_order:9001", "erp.purchase.v1",
		"PurchaseReceived", "42", payload, "IV_NO_WAREHOUSE: 没有可用仓库，无法收货"); err != nil {
		t.Fatal(err)
	}

	console := deadletter.NewConsole()
	console.Add(group, store, kafkax.ReplayHandler(PurchaseEvents(svc, log)))

	// ---- 原因还没修好就点重放：拿到原因本身，事件原地不动 ----
	err = console.Replay(ctx, group, mustOnlyParkedID(t, pool, tenantID))
	if err == nil {
		t.Fatal("仓库还没建就重放成功了——这笔收货记到哪儿去了？")
	}
	if !strings.Contains(err.Error(), "没有可用仓库") {
		t.Fatalf("重放失败要把原因原样带回：%v", err)
	}
	if n := parkedCount(t, pool, tenantID); n != 1 {
		t.Fatalf("失败的重放不该动死信，实际剩 %d 条", n)
	}

	// ---- 修好原因（建仓库），再重放：补记成功，死信离场 ----
	if _, err := pool.Exec(ctx,
		`INSERT INTO warehouses (tenant_id, code, name, wh_type) VALUES ($1,'WH01','主仓库','NORMAL')`,
		tenantID); err != nil {
		t.Fatal(err)
	}
	if err := console.Replay(ctx, group, mustOnlyParkedID(t, pool, tenantID)); err != nil {
		t.Fatalf("原因修好之后重放该成功：%v", err)
	}
	if n := parkedCount(t, pool, tenantID); n != 0 {
		t.Fatalf("重放成功后死信该离场，实际剩 %d 条", n)
	}
	var onHand string
	if err := pool.QueryRow(ctx,
		`SELECT on_hand_qty::text FROM stocks WHERE tenant_id=$1`, tenantID).Scan(&onHand); err != nil {
		t.Fatalf("重放成功了库存却没有记录：%v", err)
	}
	if !strings.HasPrefix(onHand, "10") {
		t.Fatalf("补记的数量不对：%s", onHand)
	}

	// ---- 不认识的消费组：明确拒绝，不静默 ----
	if err := console.Replay(ctx, "no-such-group", 1); err == nil {
		t.Fatal("不认识的消费组也重放成功了")
	}
}

func mustOnlyParkedID(t *testing.T, pool *pgxpool.Pool, tenantID int64) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(),
		"SELECT id FROM failed_events WHERE tenant_id=$1", tenantID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func parkedCount(t *testing.T, pool *pgxpool.Pool, tenantID int64) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM failed_events WHERE tenant_id=$1", tenantID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

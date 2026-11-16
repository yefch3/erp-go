package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestRequirementSpecAcceptsFrozenSnapshotLongerThan300Characters(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM purchase_requirements WHERE tenant_id=$1", tenantID)
	}()

	spec := strings.Repeat("完整客户规格", 70)
	payment := strings.Repeat("预付百分之三十，余款在收到提单副本后六十日内付清。", 30)
	var stored, storedPayment string
	err = pool.QueryRow(ctx, `INSERT INTO purchase_requirements
		(tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,
		 product_id,product_name,spec,uom_code,required_qty,source_payment_terms)
		VALUES ($1,1,'CT-LONG-SPEC',1,$1,0,'GALVANIZED ROLLS',$2,'TON',1,$3)
		RETURNING spec,source_payment_terms`, tenantID, spec, payment).Scan(&stored, &storedPayment)
	if err != nil {
		t.Fatal(err)
	}
	if stored != spec {
		t.Fatalf("frozen specification was changed: got %d chars, want %d", len([]rune(stored)), len([]rune(spec)))
	}
	if storedPayment != payment {
		t.Fatal("contract payment terms were changed while creating a purchase requirement")
	}
}

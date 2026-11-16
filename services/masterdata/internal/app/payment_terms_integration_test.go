package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestCustomerAndSupplierPreserveLongPaymentTerms(t *testing.T) {
	dsn := os.Getenv("MD_TEST_DSN")
	if dsn == "" {
		t.Skip("MD_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	tenantID := time.Now().UnixNano()
	for _, table := range []string{"customers", "suppliers"} {
		t.Run(table, func(t *testing.T) {
			payment := strings.Repeat("30% deposit; balance within 60 days after shipment. ", 20)
			var id int64
			var stored string
			err := tx.QueryRow(ctx, `INSERT INTO `+table+`
				(tenant_id,code,name,payment_term) VALUES($1,'LONG-PAYMENT-TERMS','Payment terms test',$2)
				RETURNING id,payment_term`, tenantID, payment).Scan(&id, &stored)
			if err != nil {
				t.Fatal(err)
			}
			if stored != payment {
				t.Fatal("saved payment terms were changed")
			}
			payment = strings.Repeat("预付百分之三十，余款在收到提单副本后六十日内付清。", 30)
			_, err = tx.Exec(ctx, `UPDATE `+table+` SET payment_term=$3 WHERE tenant_id=$1 AND id=$2`, tenantID, id, payment)
			if err != nil {
				t.Fatal(err)
			}
			if err := tx.QueryRow(ctx, `SELECT payment_term FROM `+table+` WHERE tenant_id=$1 AND id=$2`, tenantID, id).Scan(&stored); err != nil {
				t.Fatal(err)
			}
			if stored != payment {
				t.Fatal("updated payment terms were changed")
			}
		})
	}
}

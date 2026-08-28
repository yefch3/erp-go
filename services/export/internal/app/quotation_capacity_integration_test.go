package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 采购询盘是报价单的上游，报价快照不能比上游字段更窄。否则真实询盘里的
// 付款条件、港口或完整规格一长，保存阶段只会冒出一个数据库 500。
func TestSourcedQuotationFieldsFitProcurementSnapshots(t *testing.T) {
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
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM quotation_items WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM quotations WHERE tenant_id=$1", tenantID)
	})

	var quotationID int64
	err = pool.QueryRow(ctx, `INSERT INTO quotations
		(tenant_id,quote_no,customer_id,customer_name,currency,incoterm,
		 port_of_loading,port_of_discharge,payment_method,fx_rate,fx_rate_at,
		 fx_source,total_amount,base_amount)
		VALUES ($1,'QT-CAPACITY-1',9,'测试客户','USD',$2,$3,$3,$4,1,now(),
		        'TEST',100,100) RETURNING id`,
		tenantID, strings.Repeat("I", 80), strings.Repeat("P", 150), strings.Repeat("T", 200),
	).Scan(&quotationID)
	if err != nil {
		t.Fatalf("quotation header rejected a valid procurement snapshot: %v", err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO quotation_items
		(tenant_id,quotation_id,line_no,product_id,product_code,product_name,
		 spec,qty,uom_code,unit_price,amount)
		VALUES ($1,$2,1,0,'','测试产品',$3,1,$4,100,100)`,
		tenantID, quotationID, strings.Repeat("S", 800), strings.Repeat("U", 50),
	)
	if err != nil {
		t.Fatalf("quotation line rejected a valid procurement snapshot: %v", err)
	}
}

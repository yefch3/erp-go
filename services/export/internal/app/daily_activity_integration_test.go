package app

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/dailyreport"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

type quotationReportScopes struct{}

func (quotationReportScopes) VisibleEmployees(_ context.Context, viewerID int64, module string) (Visibility, error) {
	if module == dailyreport.Scope {
		return Visibility{EmployeeIDs: []int64{viewerID}}, nil
	}
	return Visibility{All: true}, nil
}

func TestDailyQuotationActivityKeepsSendingOperator(t *testing.T) {
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
	tenant := time.Now().UnixNano()
	defer func() {
		for _, table := range []string{"outbox_events", "quotations"} {
			if _, err := pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenant); err != nil {
				t.Error(err)
			}
		}
	}()

	var id int64
	err = pool.QueryRow(ctx, `INSERT INTO quotations(tenant_id, quote_no, customer_id, currency,
		fx_rate, fx_rate_at, fx_source, sales_employee_id)
		VALUES ($1,'DAILY-QA',1,'USD',1,now(),'test',12) RETURNING id`, tenant).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool, Deps{Scopes: quotationReportScopes{}, Log: slog.Default()})
	for i := 0; i < 2; i++ {
		if _, err := svc.Send(ctx, tenant, id, 10); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Respond(ctx, tenant, id, 12, "ACCEPTED", ""); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	start, end := now.Add(-time.Hour).Unix(), now.Add(time.Hour).Unix()
	sender, err := svc.DailyQuotationActivity(ctx, tenant, 10, start, end, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(sender) != 2 {
		t.Fatalf("wanted team total and one sender row: %v", sender)
	}
	for _, row := range sender {
		if row.GetQuotationsSent() != 1 || (row.GetEmployeeId() != 0 && row.GetEmployeeId() != 10) {
			t.Fatalf("unexpected count or actor: %v", row)
		}
	}
	responder, err := svc.DailyQuotationActivity(ctx, tenant, 12, start, end, "")
	if err != nil || len(responder) != 1 || responder[0].GetQuotationsSent() != 0 {
		t.Fatalf("later responder received credit: %v %v", responder, err)
	}
}

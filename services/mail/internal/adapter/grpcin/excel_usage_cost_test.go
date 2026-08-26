package grpcin

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/mail/internal/app"
)

// 客户公司自己那条用量接口**不许带金额**。
//
// 页面上不显示只挡住了眼睛：字段照样躺在 JSON 里，开一下开发者工具就看见
// 了。这条钉的是接口本身——我们付给模型厂多少钱，不是客户的事。
//
// 单价故意配上：没配单价的话这条测试怎么写都会过，等于什么也没钉住。
func TestExcelUsageDoesNotHandTheCustomerOurCost(t *testing.T) {
	dsn := os.Getenv("MAIL_TEST_DSN")
	if dsn == "" {
		t.Skip("MAIL_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	tenantID := time.Now().UnixNano()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_excel_jobs WHERE tenant_id=$1", tenantID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO mail_excel_jobs
		(tenant_id, owner_id, inbound_id, selected_text, status, input_tokens, output_tokens)
		VALUES ($1,704,1,'选中的一段','COMPLETED',120000,30000)`, tenantID); err != nil {
		t.Fatal(err)
	}

	svc := app.New(pool, app.Deps{Pricing: app.ModelPricing{
		InputPerMTok:  decimal.RequireFromString("1.25"),
		OutputPerMTok: decimal.RequireFromString("10"),
		Currency:      "USD",
	}}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	ctx = grpcx.WithOperator(ctx, grpcx.Operator{TenantID: tenantID, EmployeeID: 704})
	resp, err := New(svc).ExcelUsage(ctx, &mailv1.ExcelUsageRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetRows()) == 0 {
		t.Fatal("用量账是空的——这条测试就没在测东西了")
	}
	for _, row := range resp.GetRows() {
		if row.GetEstimatedCost() != "" || row.GetCurrency() != "" {
			t.Fatalf("客户那条接口把我们的成本带出去了：%q %q。页面不显示不算数——"+
				"字段还在 JSON 里，开一下开发者工具就看见了",
				row.GetCurrency(), row.GetEstimatedCost())
		}
		// token 数照给：那是这家公司自己的用量，不是我们的成本。
		if row.GetInputTokens() == 0 && row.GetOutputTokens() == 0 {
			t.Error("连 token 数都不给了——那是客户自己的用量，该给")
		}
	}
	// 额度照给，这才是这一页要回答的问题。
	if resp.GetQuota() == nil || resp.GetQuota().GetCurrentMonth() == "" {
		t.Error("额度没带出来，用量页那条百分比条就没数据了")
	}
}

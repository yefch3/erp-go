package app

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 智能转换的用量账（计量）。
//
// 钉两条最容易做错的：**失败也要计费**（模型答了钱就花了），以及**金额是
// 算出来的不是存的**（单价会变，存进去等于把一个会过期的判断固化成历史）。
func TestExcelUsageAccounting(t *testing.T) {
	dsn := os.Getenv("MAIL_TEST_DSN")
	if dsn == "" {
		t.Skip("MAIL_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_excel_jobs WHERE tenant_id=$1", tenantID)
	}()

	const alice, bob = 501, 502
	month := time.Now().UTC().Format("2006-01")

	// 一次任务：id 和它的归属人。
	mkJob := func(owner int64, status string) int64 {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO mail_excel_jobs
			(tenant_id, owner_id, inbound_id, selected_text, status)
			VALUES ($1,$2,1,'选中的一段',$3) RETURNING id`,
			tenantID, owner, status).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	q := store.New(pool)

	// 成功一次，累加用量。
	done := mkJob(alice, "COMPLETED")
	if _, err := q.RecordExcelJobUsage(ctx, store.RecordExcelJobUsageParams{
		ID: done, InputTokens: 12_000, OutputTokens: 3_000,
	}); err != nil {
		t.Fatal(err)
	}

	// 失败那次也花了钱：模型答了，只是答出来的东西解不开。**必须算数**——
	// 漏掉它，我们的账和模型厂的账单就对不上。
	failed := mkJob(alice, "FAILED")
	if _, err := q.RecordExcelJobUsage(ctx, store.RecordExcelJobUsageParams{
		ID: failed, InputTokens: 8_000, OutputTokens: 100,
	}); err != nil {
		t.Fatal(err)
	}

	// 重试：同一个任务再花一次，累加不覆盖。
	retried := mkJob(bob, "COMPLETED")
	for i := 0; i < 2; i++ {
		if _, err := q.RecordExcelJobUsage(ctx, store.RecordExcelJobUsageParams{
			ID: retried, InputTokens: 5_000, OutputTokens: 1_000,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// ---- 没配单价：只出 token，不出金额 ----
	rows, err := svc.ExcelUsageByMonth(ctx, tenantID, month)
	if err != nil {
		t.Fatal(err)
	}
	byOwner := map[int64]ExcelUsageRow{}
	for _, r := range rows {
		byOwner[r.OwnerID] = r
	}
	a := byOwner[alice]
	if a.Runs != 2 || a.Succeeded != 1 || a.Failed != 1 {
		t.Fatalf("alice 该是 2 次里 1 成 1 败，实际 %+v", a)
	}
	if a.InputTokens != 20_000 || a.OutputTokens != 3_100 {
		t.Fatalf("失败那次的消耗也要算进来（12000+8000 / 3000+100），实际 %d / %d",
			a.InputTokens, a.OutputTokens)
	}
	b := byOwner[bob]
	if b.InputTokens != 10_000 || b.OutputTokens != 2_000 {
		t.Fatalf("重试要累加不是覆盖，实际 %d / %d", b.InputTokens, b.OutputTokens)
	}
	if a.EstimatedCost != "" || a.Currency != "" {
		t.Fatalf("没配单价就不该给金额——空和 0 是两回事，实际 %q %q", a.EstimatedCost, a.Currency)
	}

	// ---- 配了单价：金额是算出来的 ----
	priced := New(pool, Deps{Pricing: ModelPricing{
		InputPerMTok:  decimal.RequireFromString("1.25"),
		OutputPerMTok: decimal.RequireFromString("10"),
		Currency:      "USD",
	}}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	rows, err = priced.ExcelUsageByMonth(ctx, tenantID, month)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.OwnerID != alice {
			continue
		}
		// 20000/1e6*1.25 + 3100/1e6*10 = 0.025 + 0.031 = 0.056
		if r.EstimatedCost != "0.0560" {
			t.Fatalf("估算金额算错了，该是 0.0560，实际 %q", r.EstimatedCost)
		}
		if r.Currency != "USD" {
			t.Fatalf("币种该跟着单价走，实际 %q", r.Currency)
		}
	}

	// 四位小数不是随手定的：一次转换常常几分钱，两位会把它抹成 0.00，
	// 而「一次几分钱」正是要给业务看的那个数。
	tiny := estimateCost(200, 50, ModelPricing{
		InputPerMTok:  decimal.RequireFromString("1.25"),
		OutputPerMTok: decimal.RequireFromString("10"),
	})
	if tiny == "0.0000" {
		t.Fatalf("小额被抹平了，看不出一次花多少：%s", tiny)
	}

	// 库里存的始终只有 token，没有金额——单价改了，历史不该跟着变。
	var cols int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_name='mail_excel_jobs' AND column_name IN ('cost','amount','cost_micros')`).Scan(&cols); err != nil {
		t.Fatal(err)
	}
	if cols != 0 {
		t.Fatal("库里不该存金额：token 是事实，钱是判断，判断会过期")
	}
}

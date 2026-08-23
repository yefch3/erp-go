package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

// code 取出稳定的业务错误码：断言在码上而不是在中文措辞上，改一句提示
// 不该让测试变红。
func code(err error) string {
	if apiErr, ok := err.(*apierr.Error); ok {
		return apiErr.Code
	}
	return ""
}

// 信用评级（E3 第一期）。
//
// 这套东西唯一的风险不是算错，是**烂掉**——变成一个填过一次再没人动的字段。
// 所以钉的不是「能不能存」，是那几条让它不烂的规矩：依据必填、每改一次留
// 一行、上一档记得住、列表上的当前评级和历史最新一条永远一致。
func TestCreditRating(t *testing.T) {
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
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM credit_ratings WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM customers WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM suppliers WHERE tenant_id=$1", tenantID)
	}()

	var customerID, supplierID int64
	if err := pool.QueryRow(ctx, `INSERT INTO customers (tenant_id, code, name, currency)
		VALUES ($1,'C-CREDIT','汉堡贸易','USD') RETURNING id`, tenantID).Scan(&customerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO suppliers (tenant_id, code, name, currency)
		VALUES ($1,'S-CREDIT','宁波钢厂','CNY') RETURNING id`, tenantID).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}

	svc := New(pool)
	op := func(in CreditRatingInput) CreditRatingInput {
		in.OperatorID, in.OperatorName = 77, "评级人"
		return in
	}
	gradeOf := func(table string, id int64) (string, bool) {
		var grade string
		var gradedAt *time.Time
		if err := pool.QueryRow(ctx,
			`SELECT credit_grade, credit_graded_at FROM `+table+` WHERE id=$1`, id).
			Scan(&grade, &gradedAt); err != nil {
			t.Fatal(err)
		}
		return grade, gradedAt != nil
	}

	// 说不出为什么就不给评。这是整张表存在的理由。
	if _, err := svc.RateCredit(ctx, tenantID, op(CreditRatingInput{
		PartyType: "CUSTOMER", PartyID: customerID, Grade: "A",
	})); code(err) != "MD_CREDIT_BASIS_REQUIRED" {
		t.Fatalf("没写依据就不该放行，得到 %v", err)
	}
	// 只有 A/B/C/D 四档。
	if _, err := svc.RateCredit(ctx, tenantID, op(CreditRatingInput{
		PartyType: "CUSTOMER", PartyID: customerID, Grade: "E", Basis: "随便",
	})); code(err) != "MD_CREDIT_GRADE_INVALID" {
		t.Fatalf("E 不是合法评级，得到 %v", err)
	}
	// 评一个不存在的对象要说不存在，而不是留下一条无主的历史。
	if _, err := svc.RateCredit(ctx, tenantID, op(CreditRatingInput{
		PartyType: "CUSTOMER", PartyID: 99999999, Grade: "A", Basis: "随便",
	})); code(err) != "MD_CUSTOMER_NOT_FOUND" {
		t.Fatalf("不存在的客户该报找不到，得到 %v", err)
	}
	var orphans int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM credit_ratings WHERE tenant_id=$1 AND party_id=99999999`,
		tenantID).Scan(&orphans); err != nil {
		t.Fatal(err)
	}
	if orphans != 0 {
		t.Fatalf("失败的评级不该留下历史，实际留了 %d 条", orphans)
	}

	// 首次评级：上一档是空的，不是 D——没评过和评了最低分是两回事。
	first, err := svc.RateCredit(ctx, tenantID, op(CreditRatingInput{
		PartyType: "CUSTOMER", PartyID: customerID, Grade: "A",
		Basis: "合作三年，每次都提前付款",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if first.PreviousGrade != "" {
		t.Fatalf("首次评级的上一档该是空的，实际 %q", first.PreviousGrade)
	}
	if grade, dated := gradeOf("customers", customerID); grade != "A" || !dated {
		t.Fatalf("当前评级该投影回客户行，实际 %q 有时间戳=%v", grade, dated)
	}

	// 降级：上一档记得住。「从 A 降到 C」是要有人过问的事，得在一行里读得完。
	second, err := svc.RateCredit(ctx, tenantID, op(CreditRatingInput{
		PartyType: "CUSTOMER", PartyID: customerID, Grade: "C",
		Basis:    "今年两笔各逾期 40 天以上",
		Evidence: map[string]string{"overdue_count": "2", "max_overdue_days": "47"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if second.PreviousGrade != "A" {
		t.Fatalf("降级该记住上一档 A，实际 %q", second.PreviousGrade)
	}

	// 历史：最近的在前，依据和当时的数字都留着。
	history, err := svc.ListCreditRatings(ctx, tenantID, "CUSTOMER", customerID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("该有两条历史，实际 %d", len(history))
	}
	if history[0].Grade != "C" || history[1].Grade != "A" {
		t.Fatalf("最近的该在前，实际 %s / %s", history[0].Grade, history[1].Grade)
	}
	if history[0].Evidence["max_overdue_days"] != "47" {
		t.Fatalf("评级当时的数字该留着，实际 %+v", history[0].Evidence)
	}
	if history[0].Basis == "" || history[0].RatedByName != "评级人" || history[0].RatedAt == "" {
		t.Fatalf("谁在什么时候依据什么，缺一不可：%+v", history[0])
	}
	// 列表上的当前评级永远等于历史最新一条——两者不一致时没人能判断哪个作数。
	if grade, _ := gradeOf("customers", customerID); grade != history[0].Grade {
		t.Fatalf("当前评级 %q 和历史最新一条 %q 对不上", grade, history[0].Grade)
	}

	// 供应商走同一套，而且两边互不串台。
	if _, err := svc.RateCredit(ctx, tenantID, op(CreditRatingInput{
		PartyType: "SUPPLIER", PartyID: supplierID, Grade: "B",
		Basis:    "交期基本准，去年有一批少供了 20 吨",
		Evidence: map[string]string{"on_time_rate": "", "short_supply_count": "1"},
	})); err != nil {
		t.Fatal(err)
	}
	if grade, _ := gradeOf("suppliers", supplierID); grade != "B" {
		t.Fatalf("供应商评级该是 B，实际 %q", grade)
	}
	// 客户的评级没有被供应商那次盖掉——同一张表存两类对象，串台是最该防的。
	if grade, _ := gradeOf("customers", customerID); grade != "C" {
		t.Fatalf("客户评级被供应商那次改动影响了，实际 %q", grade)
	}
	supplierHistory, err := svc.ListCreditRatings(ctx, tenantID, "SUPPLIER", supplierID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(supplierHistory) != 1 {
		t.Fatalf("供应商该只有一条历史，实际 %d", len(supplierHistory))
	}
}

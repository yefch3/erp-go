package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 新公司第一次要单号，就该拿到单号——不该撞「未配置编码规则」。
//
// 编码规则的种子全部写死 tenant_id = 1，第二家起的公司一条规则都没有：第一封
// 邮件（CAMPAIGN）、第一张报价单、第一个客户编号全部失败。和邮件后台只服务
// 第一家（#216）同一个病。修法是读侧懒播种：没规则且类型在默认表里，就补一条
// 再发号，对开户路径免疫。
func TestNumberingSeedsDefaultsForANewTenant(t *testing.T) {
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
	svc := New(pool)

	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM number_sequences WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM number_rules WHERE tenant_id=$1", tenantID)
	}()

	// ---- 全新公司，第一次要号：CAMPAIGN（用户实际撞上的那个） ----
	no, err := svc.NextNumber(ctx, tenantID, "CAMPAIGN")
	if err != nil {
		t.Fatalf("新公司发第一封邮件就该拿到编号，实际：%v", err)
	}
	if !strings.HasPrefix(no, "EM-") {
		t.Fatalf("默认规则该和第一家公司的迁移一致（EM 前缀），实际 %q", no)
	}

	// 再要一次，序号在涨——补出来的规则是真在用的，不是摆设。
	no2, err := svc.NextNumber(ctx, tenantID, "CAMPAIGN")
	if err != nil {
		t.Fatal(err)
	}
	if no2 == no {
		t.Fatalf("两次发号拿到同一个号：%q", no)
	}

	// ---- 人改过的规则永远赢：默认值只填空，不还原 ----
	if _, err := pool.Exec(ctx, `UPDATE number_rules SET prefix='MAIL' WHERE tenant_id=$1 AND biz_type='CAMPAIGN'`, tenantID); err != nil {
		t.Fatal(err)
	}
	no3, err := svc.NextNumber(ctx, tenantID, "CAMPAIGN")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(no3, "MAIL-") {
		t.Fatalf("人改过前缀之后默认值不该还原它，实际 %q", no3)
	}

	// ---- 不认识的类型照旧拒绝：拼写错误不该固化成一个真实号段 ----
	if _, err := svc.NextNumber(ctx, tenantID, "CAMPAGIN"); err == nil {
		t.Fatal("写错的类型名造出了号段——拼写错误被固化了")
	}

	// ---- 每种默认类型都发得出号：默认表和迁移不一致会在这里现形 ----
	for bizType := range defaultNumberRules {
		if _, err := svc.NextNumber(ctx, tenantID, bizType); err != nil {
			t.Fatalf("默认类型 %s 发号失败：%v", bizType, err)
		}
	}
}

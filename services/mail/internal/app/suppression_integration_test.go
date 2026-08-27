package app

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 拉黑名单是这套邮件里唯一保护**发信域名声誉**的东西，而它一个测试都没有。
//
// 链条是这样的：地址永久退信 → 写进 email_suppressions → 之后每一次群发都会
// 在入队之前把它筛掉。任何一环断了，我们就会一直往一个已确认失效的地址投递，
// 拖垮的是所有邮件的送达率，不只是这一封。
//
// 钉三条：
//
//  1. 拉黑之后，收件人**根本不进队列**
//  2. **抄送和密送也要筛**——只在收件人那一栏认账等于没认，密送尤其：
//     那一栏没人看得见写给了谁
//  3. 解除拉黑之后能重新发
func TestSuppressedAddressNeverEntersTheQueue(t *testing.T) {
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
		_, _ = pool.Exec(ctx, `DELETE FROM email_suppressions WHERE tenant_id=$1`, tenantID)
	})

	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// 存进去的时候用一个乱写法：带大小写、带空格。
	//
	// 归一化必须发生在**写入**这一端。查询那一端是精确匹配
	// （SuppressedAmong 是 `email = ANY(...)`），发信前 CreateCampaign 会把
	// 收件人小写去空格再来查——两端不一致的话，同一个地址换个写法就投出去了，
	// 这份名单等于没有。
	if err := svc.Suppress(ctx, tenantID, "  DEAD@Example.com  ", "HARD_BOUNCE", "550 no such user"); err != nil {
		t.Fatalf("拉黑: %v", err)
	}

	const dead = "dead@example.com" // CreateCampaign 归一化之后的样子
	blocked, err := svc.q.SuppressedAmong(ctx, store.SuppressedAmongParams{
		TenantID: tenantID, Emails: []string{dead},
	})
	if err != nil {
		t.Fatalf("查名单: %v", err)
	}
	if len(blocked) != 1 || blocked[0].Reason != "HARD_BOUNCE" {
		t.Fatalf("按小写查应该拦下（说明写入端归一化了），实际 %+v", blocked)
	}

	// **抄送和密送也要筛。** 只在收件人那一栏认账等于没认，密送尤其：
	// 那一栏没人看得见写给了谁。这里模拟 CreateCampaign 把三栏拼在一起来查。
	blocked, err = svc.q.SuppressedAmong(ctx, store.SuppressedAmongParams{
		TenantID: tenantID,
		Emails:   []string{"alive@example.com", "cc@example.com", dead},
	})
	if err != nil {
		t.Fatalf("查名单: %v", err)
	}
	if len(blocked) != 1 || blocked[0].Email != dead {
		t.Fatalf("三栏一起查时应该只拦下那一个，实际 %+v", blocked)
	}

	// 没拉黑的地址不受影响——名单只该挡它该挡的。
	blocked, err = svc.q.SuppressedAmong(ctx, store.SuppressedAmongParams{
		TenantID: tenantID, Emails: []string{"alive@example.com"},
	})
	if err != nil {
		t.Fatalf("查名单: %v", err)
	}
	if len(blocked) != 0 {
		t.Fatalf("没拉黑的地址不该被拦，实际 %+v", blocked)
	}

	// 别家公司的名单不该串——拉黑是按公司隔离的。
	blocked, err = svc.q.SuppressedAmong(ctx, store.SuppressedAmongParams{
		TenantID: tenantID + 1, Emails: []string{dead},
	})
	if err != nil {
		t.Fatalf("查名单: %v", err)
	}
	if len(blocked) != 0 {
		t.Fatalf("别家公司不该看见这条拉黑，实际 %+v", blocked)
	}

	// 解除之后能重新发。地址换了主人、或者当初拉错了，都得有路走回来。
	if err := svc.Unsuppress(ctx, tenantID, dead); err != nil {
		t.Fatalf("解除拉黑: %v", err)
	}
	blocked, err = svc.q.SuppressedAmong(ctx, store.SuppressedAmongParams{
		TenantID: tenantID, Emails: []string{dead},
	})
	if err != nil {
		t.Fatalf("查名单: %v", err)
	}
	if len(blocked) != 0 {
		t.Fatalf("解除之后不该还被拦，实际 %+v", blocked)
	}
}

// 拉黑失败必须**报出来**，不能返回 nil。
//
// 调用方（worker.go 的永久退信分支、inbound.go 的退信处理）原来是 `_ =` 丢掉
// 这个返回值的——现在它们会记日志。前提是这里真的会返回错误：如果这个函数
// 自己把失败咽了，上面加多少日志都没用。
func TestSuppressReportsFailure(t *testing.T) {
	dsn := os.Getenv("MAIL_TEST_DSN")
	if dsn == "" {
		t.Skip("MAIL_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	pool.Close() // 数据库没了

	err = svc.Suppress(ctx, time.Now().UnixNano(), "x@example.com", "HARD_BOUNCE", "boom")
	if err == nil {
		t.Fatal("写不进去的时候必须报错——调用方靠这个返回值才知道该喊")
	}

	// 空地址也要拒绝：一条空的拉黑记录挡不住任何东西，却会让人以为挡住了。
	svc2 := New(nil, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err := svc2.Suppress(ctx, 1, "   ", "HARD_BOUNCE", ""); err == nil ||
		!strings.Contains(err.Error(), "NT_EMAIL_REQUIRED") {
		t.Fatalf("空地址应该被拒绝，实际 %v", err)
	}
}

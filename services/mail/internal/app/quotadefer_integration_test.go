package app

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 被配额限速，不该消耗重试预算。
//
// 认领时会先给 attempt_count 加一 —— 那一行是"曾经尝试过"的唯一证据，进程若在
// 调用邮件服务器途中崩溃，只有它说明发生过什么。但配额检查在拨号之前，一通电话
// 都没打出去，就不该记在账上。
//
// 原先这里走的是 MarkRetryable，它不退还那一次。后果：500 人的群发在每小时 100
// 封的配额下，靠后的几百封必然被推迟四五次，attempt_count 悄悄爬到上限；此后第
// 一次真实的临时故障就会把它判成"重试 5 次仍未成功，请人工处理" —— 而它一次都
// 没真正重试过。
//
// 运行：MAIL_TEST_DSN=postgres://erp_mail:...@127.0.0.1:5433/erp_mail
func TestQuotaDeferralRefundsTheAttempt(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := scratchTenant(t, ctx, pool)
	q := store.New(pool)

	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, "DELETE FROM email_messages WHERE tenant_id = $1", tenantID); err != nil {
			t.Errorf("清理 email_messages: %v", err)
		}
	})

	id := seedQueuedMessage(t, ctx, pool, tenantID, 3)

	if err := q.DeferForQuota(ctx, store.DeferForQuotaParams{
		TenantID: tenantID, ID: id, LastError: "已达本小时发送上限（100 封），排队等待",
		BackoffSeconds: 600,
	}); err != nil {
		t.Fatalf("配额推迟失败: %v", err)
	}

	var attempts int32
	var status string
	var hasRetryAt bool
	if err := pool.QueryRow(ctx,
		`SELECT attempt_count, status, next_retry_at IS NOT NULL
		 FROM email_messages WHERE id = $1`, id).Scan(&attempts, &status, &hasRetryAt); err != nil {
		t.Fatal(err)
	}

	if attempts != 2 {
		t.Errorf("attempt_count = %d，期望 2（认领扣的那一次要退回来）", attempts)
	}
	if status != "QUEUED" {
		t.Errorf("status = %q，期望 QUEUED", status)
	}
	if !hasRetryAt {
		t.Error("next_retry_at 为空——推迟必须落在时间上，否则下一轮立刻又被认领")
	}
}

// 计数已经是 0 时不能退成 -1：并发或历史数据都可能让它发生，而负数会让
// backoff[m.AttemptCount-1] 越界。
func TestQuotaDeferralNeverGoesNegative(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := scratchTenant(t, ctx, pool)
	q := store.New(pool)

	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, "DELETE FROM email_messages WHERE tenant_id = $1", tenantID); err != nil {
			t.Errorf("清理 email_messages: %v", err)
		}
	})

	id := seedQueuedMessage(t, ctx, pool, tenantID, 0)

	if err := q.DeferForQuota(ctx, store.DeferForQuotaParams{
		TenantID: tenantID, ID: id, LastError: "配额", BackoffSeconds: 60,
	}); err != nil {
		t.Fatalf("配额推迟失败: %v", err)
	}

	var attempts int32
	if err := pool.QueryRow(ctx,
		`SELECT attempt_count FROM email_messages WHERE id = $1`, id).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 0 {
		t.Errorf("attempt_count = %d，期望 0", attempts)
	}
}

func seedQueuedMessage(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	tenantID int64, attempts int32) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO email_messages
		   (tenant_id, message_key, sender_id, to_email, subject, body, status, attempt_count)
		 VALUES ($1, $2, 7001, 'buyer@example.com', '报价', '正文', 'SENDING', $3)
		 RETURNING id`,
		tenantID, uuid.NewString(), attempts).Scan(&id); err != nil {
		t.Fatalf("播种待发邮件: %v", err)
	}
	return id
}

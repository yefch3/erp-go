package app

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// 一个存储位置只许一行附件指着——由数据库兜底（迁移 00071），不靠谁记得
// 把序号写进位置里。
//
// 2026-09-20：位置公式漏了序号，同一封信里两个都叫 image.png 的部件算出
// 同一个位置，对象存储按位置覆盖写、不报错，149 KB 的船舶动态图被 4.3 KB
// 的签名 logo 盖掉。公式修了、数据补了，这条测试钉的是「再这么写，库会拒」。
func TestTwoRowsCannotPointAtOneStoredFile(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := scratchTenant(t, ctx, pool)
	const accountID, ownerID = 7901, 7902

	var inboundID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO email_inbound (tenant_id, account_id, owner_id, folder, imap_uid, message_id, raw_key, subject)
		VALUES ($1, $2, $3, 'INBOX', 3, 'unique@example.com', '', 'unique key') RETURNING id`,
		tenantID, accountID, ownerID).Scan(&inboundID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM email_inbound WHERE tenant_id = $1`, tenantID)
	})

	insert := func(key string, size int64) error {
		_, err := pool.Exec(ctx, `
			INSERT INTO email_inbound_attachments
			  (tenant_id, inbound_id, file_name, content_type, file_size, file_key, content_id)
			VALUES ($1, $2, 'image.png', 'image/png', $3, $4, '')`,
			tenantID, inboundID, size, key)
		return err
	}

	key := fmt.Sprintf("mail/inbound/%d/%d/att/%d-1-image.png", tenantID, accountID, inboundID)
	if err := insert(key, 149246); err != nil {
		t.Fatalf("第一行该能写进去：%v", err)
	}

	// 第二行指同一个位置：这正是当初悄悄发生的事，现在必须当场报错。
	err := insert(key, 4321)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("两行指同一个位置应该被唯一约束拒掉，实际 %v", err)
	}

	// 带了序号的第二个位置照常能写。
	second := fmt.Sprintf("mail/inbound/%d/%d/att/%d-2-image.png", tenantID, accountID, inboundID)
	if err := insert(second, 4321); err != nil {
		t.Fatalf("第二个位置该能写进去：%v", err)
	}

	// 「从来没存过」可以有很多行：超大附件、0 字节的空部件都是空位置。
	for i := 0; i < 3; i++ {
		if err := insert("", 0); err != nil {
			t.Fatalf("空位置的行该能重复：%v", err)
		}
	}
}

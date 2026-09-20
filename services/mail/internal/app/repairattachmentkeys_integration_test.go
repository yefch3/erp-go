package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
)

// 补数据的工具要真的把被覆盖的文件摆回去，而且只碰撞在一起的那些。
//
// 这段用真库：要证明的是「库里两行指同一个位置 → 跑完之后各指各的、内容
// 也各是各的」，编一个假的 pool 等于测我自己写的假 SQL。

// oneMessageWithTwoSameNamedImages 造一封信：正文两张内嵌图，都叫
// image.png，一大一小——就是 2026-09-20 那封船期更新的形状。
const oneMessageWithTwoSameNamedImages = "Subject: vessel status\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/related; boundary=\"B\"\r\n" +
	"\r\n" +
	"--B\r\n" +
	"Content-Type: text/html; charset=utf-8\r\n" +
	"\r\n" +
	"<div><img src=\"cid:big\"><img src=\"cid:small\"></div>\r\n" +
	"--B\r\n" +
	"Content-Type: image/png; name=\"image.png\"\r\n" +
	"Content-Disposition: inline; filename=\"image.png\"\r\n" +
	"Content-ID: <big>\r\n" +
	"Content-Transfer-Encoding: base64\r\n" +
	"\r\n" +
	// 「船舶动态图」：解出来是 VESSEL
	"VkVTU0VM\r\n" +
	"--B\r\n" +
	"Content-Type: image/png; name=\"image.png\"\r\n" +
	"Content-Disposition: inline; filename=\"image.png\"\r\n" +
	"Content-ID: <small>\r\n" +
	"Content-Transfer-Encoding: base64\r\n" +
	"\r\n" +
	// 「签名 logo」：解出来是 LOGO
	"TE9HTw==\r\n" +
	"--B--\r\n"

func TestRepairPutsOverwrittenAttachmentsBackInTheirOwnPlace(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := scratchTenant(t, ctx, pool)
	const accountID, ownerID = 7701, 7702

	files := &previewStore{objects: map[string][]byte{}}
	svc := New(pool, Deps{Files: files}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	rawKey := fmt.Sprintf("mail/inbound/%d/%d/INBOX/1/1.eml", tenantID, accountID)
	files.objects[rawKey] = []byte(oneMessageWithTwoSameNamedImages)

	var inboundID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO email_inbound (tenant_id, account_id, owner_id, folder, imap_uid, message_id, raw_key, subject)
		VALUES ($1, $2, $3, 'INBOX', 1, 'vessel@example.com', $4, 'vessel status') RETURNING id`,
		tenantID, accountID, ownerID, rawKey).Scan(&inboundID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM email_inbound WHERE tenant_id = $1`, tenantID)
	})

	// 出事时的样子：两行指着同一个位置，那个位置上躺着后写的 logo。
	collided := fmt.Sprintf("mail/inbound/%d/%d/att/%d-image.png", tenantID, accountID, inboundID)
	files.objects[collided] = []byte("LOGO")
	for _, cid := range []string{"big", "small"} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO email_inbound_attachments
			  (tenant_id, inbound_id, file_name, content_type, file_size, file_key, content_id)
			VALUES ($1, $2, 'image.png', 'image/png', 6, $3, $4)`,
			tenantID, inboundID, collided, cid); err != nil {
			t.Fatal(err)
		}
	}

	// 先只看不动：该报出有东西要补，但一个字节都不许写。
	before := len(files.objects)
	dry, err := svc.RepairInboundAttachmentKeys(ctx, RepairOptions{TenantID: tenantID, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if dry.FilesRestored != 2 {
		t.Fatalf("只看不动时报出 %d 个要补，想要 2", dry.FilesRestored)
	}
	if len(files.objects) != before {
		t.Fatalf("只看不动却写了东西：%d → %d", before, len(files.objects))
	}

	// 动真格的。
	rep, err := svc.RepairInboundAttachmentKeys(ctx, RepairOptions{TenantID: tenantID})
	if err != nil {
		t.Fatal(err)
	}
	if rep.FilesRestored != 2 || rep.MessagesRepaired != 1 || rep.MessagesSkipped != 0 {
		t.Fatalf("补了 %d 个文件 / %d 封信 / 跳过 %d，想要 2 / 1 / 0",
			rep.FilesRestored, rep.MessagesRepaired, rep.MessagesSkipped)
	}

	// 两行各指各的位置，而且内容对得上：大的那张是船舶图，小的是 logo。
	rows, err := pool.Query(ctx,
		`SELECT content_id, file_key FROM email_inbound_attachments WHERE inbound_id = $1 ORDER BY id`, inboundID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var cid, key string
		if err := rows.Scan(&cid, &key); err != nil {
			t.Fatal(err)
		}
		got[cid] = key
	}
	if got["big"] == got["small"] {
		t.Fatalf("补完之后两行还指着同一个位置：%s", got["big"])
	}
	if s := string(files.objects[got["big"]]); s != "VESSEL" {
		t.Fatalf("船舶图那一行指到的内容是 %q，想要 VESSEL", s)
	}
	if s := string(files.objects[got["small"]]); s != "LOGO" {
		t.Fatalf("logo 那一行指到的内容是 %q，想要 LOGO", s)
	}

	// 再跑一次不该有任何动作——补数据的工具会被重复执行，这是常态。
	again, err := svc.RepairInboundAttachmentKeys(ctx, RepairOptions{TenantID: tenantID})
	if err != nil {
		t.Fatal(err)
	}
	if again.FilesRestored != 0 || again.MessagesSeen != 0 {
		t.Fatalf("第二次跑还想补 %d 个文件 / 看到 %d 封信，应该都是 0",
			again.FilesRestored, again.MessagesSeen)
	}
}

// 原件没留的信补不回来，但不能因此把整趟活儿带崩，也不能乱动那一行。
func TestRepairSkipsAMessageWhoseOriginalIsGone(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := scratchTenant(t, ctx, pool)
	const accountID, ownerID = 7801, 7802

	files := &previewStore{objects: map[string][]byte{}}
	svc := New(pool, Deps{Files: files}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	var inboundID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO email_inbound (tenant_id, account_id, owner_id, folder, imap_uid, message_id, raw_key, subject)
		VALUES ($1, $2, $3, 'INBOX', 2, 'gone@example.com', '', 'no original') RETURNING id`,
		tenantID, accountID, ownerID).Scan(&inboundID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM email_inbound WHERE tenant_id = $1`, tenantID)
	})
	collided := fmt.Sprintf("mail/inbound/%d/%d/att/%d-image.png", tenantID, accountID, inboundID)
	for _, cid := range []string{"a", "b"} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO email_inbound_attachments
			  (tenant_id, inbound_id, file_name, content_type, file_size, file_key, content_id)
			VALUES ($1, $2, 'image.png', 'image/png', 6, $3, $4)`,
			tenantID, inboundID, collided, cid); err != nil {
			t.Fatal(err)
		}
	}

	rep, err := svc.RepairInboundAttachmentKeys(ctx, RepairOptions{TenantID: tenantID})
	if err != nil {
		t.Fatalf("一封补不了的信不该让整趟活儿报错：%v", err)
	}
	if rep.MessagesSkipped != 1 || rep.FilesRestored != 0 {
		t.Fatalf("跳过 %d 封 / 补了 %d 个，想要 1 / 0", rep.MessagesSkipped, rep.FilesRestored)
	}
	if len(rep.Problems) != 1 {
		t.Fatalf("跳过的信要说清原因，实际记了 %d 条", len(rep.Problems))
	}
	// 那两行原样没动。
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM email_inbound_attachments WHERE inbound_id = $1 AND file_key = $2`,
		inboundID, collided).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("补不了的行被动过了：还剩 %d 行指着原位置，想要 2", n)
	}
}

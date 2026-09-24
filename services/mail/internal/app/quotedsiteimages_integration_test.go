package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// 回信引用了一张从客户网站缓存下来的图：跟着信走。原来发出去的是一条一小时就
// 过期的存储地址，客户过一阵打开就是裂图（2026-09 发出的 289 封里有 26 封）。
// 43 字节的追踪像素照带不误：带在信里它只是一张图，不会替谁报告「已读」。
func TestAQuotedPictureCachedFromTheSendersSiteTravelsWithTheMail(t *testing.T) {
	f := newFolderFixture(t, 9431)
	ctx := context.Background()
	files := &readRecordingFiles{data: tinyPNG}
	f.svc.files = files

	id := f.insertMail(t, "INBOX", 801, "site-pictures")
	logo := fmt.Sprintf("mail/inbound-img/%d/%d/logo.png", f.tenantID, id)
	pixel := fmt.Sprintf("mail/inbound-img/%d/%d/pixel.gif", f.tenantID, id)
	f.insertCachedImage(t, id, "https://buyer.example/logo.png", logo)
	f.insertCachedImage(t, id, "https://url4715.sender.example/open.gif", pixel)

	body := quotingBody(logo) + quotingBody(pixel)
	out, inline := f.svc.InlineMailImages(ctx, f.tenantID, f.me, body)
	if len(inline) != 2 {
		t.Fatalf("carried %d parts, want 2 (the pixel included)", len(inline))
	}
	if strings.Contains(out, "X-Amz-Signature") {
		t.Errorf("an expiring link is still in the body:\n%s", out)
	}
}

// 超出一封信能带的量：网站副本改回发件人的原地址（至少不会过期），正文自带的
// 图没有原地址可退，照旧留着那条链接——指向一个不存在的部件比链接更糟。
func TestBeyondTheBudgetASitePictureGoesBackToTheSendersAddress(t *testing.T) {
	f := newFolderFixture(t, 9432)
	ctx := context.Background()
	f.svc.files = &readRecordingFiles{data: tinyPNG}

	id := f.insertMail(t, "INBOX", 802, "newsletter")
	var body strings.Builder
	for i := 1; i <= quotedImagesMaxCount+1; i++ {
		key := fmt.Sprintf("mail/inbound-img/%d/%d/%02d.png", f.tenantID, id, i)
		f.insertCachedImage(t, id, fmt.Sprintf("https://sender.example/%02d.png", i), key)
		body.WriteString(quotingBody(key))
	}
	inlineKey := fmt.Sprintf("mail/inbound-img/%d/%d/inline.png", f.tenantID, id)
	f.insertCachedImage(t, id, dataImageKey(dataURL("image/png", tinyPNG)), inlineKey)
	body.WriteString(quotingBody(inlineKey))

	out, inline := f.svc.InlineMailImages(ctx, f.tenantID, f.me, body.String())
	if len(inline) != quotedImagesMaxCount {
		t.Fatalf("carried %d, want %d", len(inline), quotedImagesMaxCount)
	}
	last := fmt.Sprintf(`src="https://sender.example/%02d.png"`, quotedImagesMaxCount+1)
	if !strings.Contains(out, last) {
		t.Errorf("the picture over budget did not go back to the sender's address:\n%.400s", out)
	}
	if !strings.Contains(out, inlineKey+"?X-Amz-Signature") {
		t.Errorf("a picture with no address of its own was rewritten to something else")
	}
}

// 「已发送」里我们自己那份、客户回信里引用的我们那条：当时签的地址早过期了，
// 打开时按库里本人名下的行签一条新的。别人名下的 key 不签。
func TestAStaleLinkToOurStorageIsReSignedWhenReading(t *testing.T) {
	f := newFolderFixture(t, 9433)
	ctx := context.Background()
	f.svc.files = &previewStore{objects: map[string][]byte{}}

	orig := f.insertMail(t, "INBOX", 803, "their-mail")
	mine := fmt.Sprintf("mail/inbound-img/%d/%d/logo.png", f.tenantID, orig)
	f.insertCachedImage(t, orig, "https://buyer.example/logo.png", mine)

	theirs := "mail/inbound-img/1/1/secret.png"
	sent := f.insertMail(t, "SENT", 804, "our-reply")
	f.setBody(t, sent, `<p>好的</p>`+quotingBody(mine)+quotingBody(theirs))

	v, err := f.svc.GetInbound(ctx, f.tenantID, f.me, sent)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(v.BodyHTML+v.QuotedHTML, `src="https://files.example/`+mine+`?ct=image/png"`) {
		t.Errorf("our own picture was not re-signed:\n%s\n%s", v.BodyHTML, v.QuotedHTML)
	}
	if strings.Contains(v.BodyHTML+v.QuotedHTML, "files.example/"+theirs) {
		t.Errorf("a key that is not ours was signed")
	}
}

// 会话视图里同样：我们发出的那条、客户回信里引用我们的那条，都换成新签的。
func TestTheThreadReSignsQuotedSitePicturesInBothDirections(t *testing.T) {
	f := newFolderFixture(t, 9434)
	ctx := context.Background()
	f.svc.files = &previewStore{objects: map[string][]byte{}}
	const thread = "resign-thread"

	first := f.insertThreadMail(t, "INBOX", 805, "报价", thread)
	logo := fmt.Sprintf("mail/inbound-img/%d/%d/logo.png", f.tenantID, first)
	f.insertCachedImage(t, first, "https://buyer.example/logo.png", logo)

	if _, err := f.pool.Exec(ctx, `INSERT INTO email_messages
		(tenant_id, message_key, sender_id, sender_name, from_email, to_email,
		 subject, body, body_format, thread_key, status, queued_at, sent_at)
		VALUES ($1, gen_random_uuid(), $2, '我', 'me@263.net', 'ana@buyer.example',
		        '回复：报价', $3, 'HTML', $4, 'ACCEPTED', now(), now())`,
		f.tenantID, f.me, `<p>好的</p>`+quotingBody(logo), thread); err != nil {
		t.Fatal(err)
	}
	reply := f.insertThreadMail(t, "INBOX", 806, "回复：回复：报价", thread)
	f.setBody(t, reply, `<p>谢谢</p>`+quotingBody(logo))

	items, err := f.svc.GetMailThread(ctx, f.tenantID, f.me, 0, thread)
	if err != nil {
		t.Fatal(err)
	}
	fresh := "files.example/" + logo + "?ct=image/png"
	seen := 0
	for _, it := range items {
		all := it.Body + it.Quoted
		if strings.Contains(all, "X-Amz-Signature") {
			t.Errorf("%s turn %d still carries the expired link:\n%s", it.Direction, it.ID, all)
		}
		if strings.Contains(all, fresh) {
			seen++
		}
	}
	if seen != 2 {
		t.Errorf("%d turns show the re-signed picture, want 2", seen)
	}
}

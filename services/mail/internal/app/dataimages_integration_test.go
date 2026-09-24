package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func (f *folderFixture) setBody(t *testing.T, id int64, html string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE email_inbound SET body_html = $3, images_cached_at = NULL WHERE tenant_id = $1 AND id = $2`,
		f.tenantID, id, html); err != nil {
		t.Fatal(err)
	}
}

func (f *folderFixture) cachedImageCount(t *testing.T, id int64) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM email_inbound_images WHERE tenant_id = $1 AND inbound_id = $2`,
		f.tenantID, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// 一个外链图片服务器，记下被取了几次。
func countingImageServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(tinyPNG)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// Foxmail 粘进正文的截图：缓存那一遍存进存储，单封打开和会话里看到的都是
// 存好的那一份，正文里不再背着那十几万个字符。
func TestAPictureCarriedInTheBodyIsCachedAndShown(t *testing.T) {
	f := newFolderFixture(t, 9421)
	ctx := context.Background()
	files := &previewStore{objects: map[string][]byte{}}
	f.svc.files = files

	id := f.insertMail(t, "INBOX", 701, "foxmail-shot")
	f.setBody(t, id, `<p>见截图</p><img alt="" src="`+dataURL("image/png", tinyPNG)+`">`)

	if _, err := f.svc.cacheImagesOnce(ctx, f.tenantID, http.DefaultClient, ""); err != nil {
		t.Fatal(err)
	}
	if len(files.objects) != 1 {
		t.Fatalf("stored %d objects, want 1", len(files.objects))
	}

	v, err := f.svc.GetInbound(ctx, f.tenantID, f.me, id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(v.BodyHTML, `src="https://files.example/mail/inbound-img/`) || strings.Contains(v.BodyHTML, "base64") {
		t.Errorf("single view does not show the stored copy: %.300s", v.BodyHTML)
	}

	items, err := f.svc.GetMailThread(ctx, f.tenantID, f.me, id, "foxmail-shot-thr")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !strings.Contains(items[0].Body, `src="https://files.example/mail/inbound-img/`) {
		t.Errorf("thread view does not show the stored copy: %+v", items)
	}
}

// 迁移 00077 把带自带图的旧信重新排进队列。已经存过的外链图片不能再取一遍：
// 再取就是让对方服务器再记一次「这封信被打开了」。
func TestRequeuingAMessageDoesNotFetchItsPicturesAgain(t *testing.T) {
	f := newFolderFixture(t, 9422)
	ctx := context.Background()
	files := &previewStore{objects: map[string][]byte{}}
	f.svc.files = files
	srv, hits := countingImageServer(t)

	id := f.insertMail(t, "INBOX", 702, "requeued")
	f.setBody(t, id, `<img src="`+srv.URL+`/logo.png"><img src="`+dataURL("image/png", tinyPNG)+`">`)

	for pass := 1; pass <= 2; pass++ {
		if _, err := f.svc.cacheImagesOnce(ctx, f.tenantID, srv.Client(), ""); err != nil {
			t.Fatal(err)
		}
		if _, err := f.pool.Exec(ctx, `UPDATE email_inbound SET images_cached_at = NULL WHERE tenant_id = $1 AND id = $2`, f.tenantID, id); err != nil {
			t.Fatal(err)
		}
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("the sender's server was asked %d times, want 1", got)
	}
	if got := f.cachedImageCount(t, id); got != 2 {
		t.Errorf("%d pictures recorded, want 2", got)
	}
}

func (f *folderFixture) insertCachedImage(t *testing.T, inboundID int64, source, key string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO email_inbound_images
		(tenant_id, inbound_id, source_url, url_hash, object_key, content_type, byte_size)
		VALUES ($1, $2, $3, $4, $5, 'image/png', 4)`,
		f.tenantID, inboundID, source, urlHash(source), key); err != nil {
		t.Fatal(err)
	}
}

func quotingBody(key string) string {
	return `<p>好的</p><blockquote><img src="https://bucket.s3.us-west-2.amazonaws.com/` +
		key + `?X-Amz-Signature=EXPIRED" alt="image"></blockquote>`
}

// 回信引用了一张正文自带、已经存进存储的图：跟着信走，不留一条会过期的地址。
// 它没有别的地址可退——不带着走，客户过几天看到的就是一个裂图。
func TestAQuotedInlinePictureTravelsWithTheMail(t *testing.T) {
	f := newFolderFixture(t, 9423)
	ctx := context.Background()
	files := &readRecordingFiles{data: tinyPNG}
	f.svc.files = files

	id := f.insertMail(t, "INBOX", 703, "quoted-inline")
	key := fmt.Sprintf("mail/inbound-img/%d/%d/abcd.png", f.tenantID, id)
	f.insertCachedImage(t, id, dataImageKey(dataURL("image/png", tinyPNG)), key)

	out, inline := f.svc.InlineMailImages(ctx, f.tenantID, f.me, quotingBody(key))
	if len(inline) != 1 {
		t.Fatalf("carried %d parts, want 1", len(inline))
	}
	if !strings.Contains(out, "cid:"+inline[0].ContentID) || strings.Contains(out, "X-Amz-Signature") {
		t.Errorf("the expiring link is still in the body:\n%s", out)
	}
}

// 正文说了不算，库说了算：别人名下那封信里的自带图，根本不去读。
func TestSomebodyElsesInlinePictureIsNeverRead(t *testing.T) {
	f := newFolderFixture(t, 9425)
	ctx := context.Background()
	files := &readRecordingFiles{data: tinyPNG}
	f.svc.files = files

	var theirs int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO email_inbound
		(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid, from_email, to_email, subject, received_at)
		VALUES ($1, $2, $3, 'theirs@mid', 'theirs-thr', 'INBOX', 705, 'c@x', 'them@263.net', 'theirs', now()) RETURNING id`,
		f.tenantID, f.account, f.me+1).Scan(&theirs); err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("mail/inbound-img/%d/%d/cafe.png", f.tenantID, theirs)
	f.insertCachedImage(t, theirs, dataImageKey(dataURL("image/png", tinyPNG)), key)

	if _, inline := f.svc.InlineMailImages(ctx, f.tenantID, f.me, quotingBody(key)); len(inline) != 0 || len(files.got) != 0 {
		t.Errorf("somebody else's picture was carried (%d) or read (%v)", len(inline), files.got)
	}
}

package app

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

// readRecordingFiles 记下每次真的去存储读了哪个 key。
//
// 越权测试真正要问的是「有没有去读」，不是「读回来的对不对」——伤害在读那一下
// 就已经发生了。
type readRecordingFiles struct {
	Files
	got  []string
	data []byte
}

func (f *readRecordingFiles) Get(_ context.Context, key string) (io.ReadCloser, error) {
	f.got = append(f.got, key)
	return io.NopCloser(bytes.NewReader(f.data)), nil
}

func (f *folderFixture) insertAttachment(t *testing.T, inboundID int64, key string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO email_inbound_attachments
		(tenant_id, inbound_id, file_name, content_type, file_size, file_key, content_id)
		VALUES ($1, $2, 'image.png', 'image/png', 4, $3, '')`,
		f.tenantID, inboundID, key); err != nil {
		t.Fatal(err)
	}
}

// 回复里引用的那张图，跟着信走。
//
// 写信框把阅读视图那段 HTML 抄进 blockquote，里面的 <img src> 是一条限时的
// 存储地址。不管它，那条地址就这么发给了客户，过几天裂开。这里断言它变成了
// 随信携带的部件。
func TestAQuotedPictureTravelsWithTheMail(t *testing.T) {
	f := newFolderFixture(t, 9411)
	ctx := context.Background()
	files := &readRecordingFiles{data: []byte("PNG!")}
	f.svc.files = files

	mailID := f.insertMail(t, "INBOX", 601, "带图的来信")
	key := "mail/inbound/7/23/att/19346-image.png"
	f.insertAttachment(t, mailID, key)

	body := `<p>好的</p><blockquote><img src="https://bucket.s3.us-west-2.amazonaws.com/` +
		key + `?X-Amz-Signature=EXPIRED" alt="image"></blockquote>`

	out, inline := f.svc.InlineMailImages(ctx, f.tenantID, f.me, body)

	if len(inline) != 1 {
		t.Fatalf("引用里的图没有跟着信走，带了 %d 个部件", len(inline))
	}
	if !strings.Contains(out, "cid:"+inline[0].ContentID) {
		t.Errorf("正文没改成指向那个部件：\n%s", out)
	}
	if strings.Contains(out, "X-Amz-Signature") {
		t.Errorf("会过期的地址还留在正文里：\n%s", out)
	}
	if string(inline[0].Data) != "PNG!" {
		t.Errorf("带上的不是那张图的内容：%q", inline[0].Data)
	}
}

// 正文说了不算，库说了算。
//
// <img src> 是人能手打的东西。照着它去开一个对象，等于让外面的人点名读存储里
// 的任意一个 key——写一条指向同事（或别的租户）附件的地址，那张图就会被读出来
// 塞进自己发出去的信里。
//
// 所以断言的是**根本没去读**，不是「读了但没用」：越权在读那一下就已经发生。
func TestAKeyThatIsNotYoursIsNeverEvenRead(t *testing.T) {
	f := newFolderFixture(t, 9412)
	ctx := context.Background()
	files := &readRecordingFiles{data: []byte("SECRET")}
	f.svc.files = files

	// 同一个租户里，别人名下的信和它的附件。
	otherOwner := f.me + 1
	var otherMail int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO email_inbound
		(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
		 from_email, to_email, subject, received_at)
		VALUES ($1, $2, $3, 'other@mid', 'other-thr', 'INBOX', 777,
		        'c@x', 'other@263.net', '别人的信', now()) RETURNING id`,
		f.tenantID, f.account, otherOwner).Scan(&otherMail); err != nil {
		t.Fatal(err)
	}
	otherKey := "mail/inbound/7/23/att/999-secret.png"
	f.insertAttachment(t, otherMail, otherKey)

	body := `<p>看这个</p><img src="https://bucket.s3.us-west-2.amazonaws.com/` +
		otherKey + `?X-Amz-Signature=X" alt="image">`

	out, inline := f.svc.InlineMailImages(ctx, f.tenantID, f.me, body)

	if len(files.got) != 0 {
		t.Errorf("去存储读了不属于这个人的 key：%v", files.got)
	}
	if len(inline) != 0 {
		t.Errorf("把别人的附件带进了信里：%d 个部件", len(inline))
	}
	if out != body {
		t.Errorf("正文被改了，说明那条地址被当成自己的处理了：\n%s", out)
	}
}

// 别人网站上的图不碰。
func TestSomebodyElsesPictureIsLeftAlone(t *testing.T) {
	f := newFolderFixture(t, 9413)
	files := &readRecordingFiles{data: []byte("PNG!")}
	f.svc.files = files

	body := `<img src="https://customer.example.com/img/mail/logo.png">`
	out, inline := f.svc.InlineMailImages(context.Background(), f.tenantID, f.me, body)

	if len(inline) != 0 || out != body {
		t.Errorf("改了客户自己网站上的图：\n%s", out)
	}
	if len(files.got) != 0 {
		t.Errorf("为了一张外站的图去读了存储：%v", files.got)
	}
}

// ourStorageKey 只负责挑候选，挑宽一点没关系——真正的闸在库那一步。
func TestOurStorageKeyPicksTheObjectOutOfTheAddress(t *testing.T) {
	cases := map[string]string{
		// 虚拟主机式
		"https://bucket.s3.us-west-2.amazonaws.com/mail/inbound/1/2/att/3-a.png?X-Amz-Signature=x": "mail/inbound/1/2/att/3-a.png",
		// 路径式：桶名在路径第一段
		"https://s3.example.com/bucket/mail/inbound/1/2/att/3-a.png": "mail/inbound/1/2/att/3-a.png",
		// 不是我们的
		"https://customer.example.com/logo.png": "",
		"":                                      "",
		"not a url at all":                      "",
	}
	for in, want := range cases {
		if got := ourStorageKey(in); got != want {
			t.Errorf("ourStorageKey(%q) = %q，想要 %q", in, got, want)
		}
	}
}

// 同一张图引两次，带一份。
func TestOnePictureQuotedTwiceIsCarriedOnce(t *testing.T) {
	f := newFolderFixture(t, 9414)
	ctx := context.Background()
	files := &readRecordingFiles{data: []byte("PNG!")}
	f.svc.files = files

	mailID := f.insertMail(t, "INBOX", 602, "带图的来信")
	key := "mail/inbound/7/23/att/12-image.png"
	f.insertAttachment(t, mailID, key)

	src := `https://bucket.s3.us-west-2.amazonaws.com/` + key
	body := `<img src="` + src + `?sig=a"><p>中间</p><img src="` + src + `?sig=b">`

	out, inline := f.svc.InlineMailImages(ctx, f.tenantID, f.me, body)

	if len(inline) != 1 {
		t.Fatalf("同一张图带了 %d 份", len(inline))
	}
	if n := strings.Count(out, "cid:"+inline[0].ContentID); n != 2 {
		t.Errorf("两处引用没有都指向那一份，只改了 %d 处：\n%s", n, out)
	}
}

// 回头看自己发出去的信，引用里的图也要看得见。
//
// 存下来的正文是当时写的那一份（有意如此），里面那条签名地址早过期了。会话
// 视图按对象 key 换成刚签的一条。
func TestOurOwnSentMailGetsAFreshPictureAddressOnRead(t *testing.T) {
	key := "mail/inbound/7/23/att/19346-image.png"
	body := `<blockquote><img src="https://bucket.s3.amazonaws.com/` + key +
		`?X-Amz-Signature=EXPIRED" alt="image"></blockquote>`
	fresh := map[string]string{key: "https://files.example/" + key + "?sig=FRESH"}

	out := refreshStorageImageLinks(body, fresh)

	if strings.Contains(out, "EXPIRED") {
		t.Errorf("过期的地址还在：\n%s", out)
	}
	if !strings.Contains(out, "sig=FRESH") {
		t.Errorf("没换成新签的地址：\n%s", out)
	}

	// 白名单里没有的，一律不碰——照着正文里的地址去签，就是替外面的人签任意 key。
	untouched := `<img src="https://bucket.s3.amazonaws.com/mail/inbound/9/9/att/1-x.png?sig=OLD">`
	if got := refreshStorageImageLinks(untouched, fresh); got != untouched {
		t.Errorf("换了一个不在白名单里的 key：\n%s", got)
	}
}

// 会话里每一行的「谁写的、写给谁的」，两个方向必须是同一个意思。
//
// 原来界面上那一列是 counterparty，而它在两腿上说的不是同一件事：我发出的
// 那行它是收件人，收到的那行它是发件人。于是一条会话里上下两行的地址，一个
// 是「发给谁」、一个是「谁发的」，两行还都标着「我发出」（发信地址是自己名下
// 的箱时就会这样）——看的人无从分辨。这条钉住新的四个字段两腿含义一致。
func TestEveryTurnSaysWhoWroteItAndWhoItWentTo(t *testing.T) {
	f := newFolderFixture(t, 9511)
	ctx := context.Background()
	const thread = "who-wrote-what"

	// 收到的一封：客户发给我们，还抄送了一个人。
	if _, err := f.pool.Exec(ctx, `INSERT INTO email_inbound
		(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
		 from_email, from_name, to_email, to_all, cc, subject, body_text, received_at)
		VALUES ($1, $2, $3, 'in@mid', $4, 'INBOX', 901,
		        'ana@buyer.example', 'Ana Costa', 'me@263.net',
		        'me@263.net, colleague@263.net', 'boss@buyer.example',
		        '报价', '正文', now())`,
		f.tenantID, f.account, f.me, thread); err != nil {
		t.Fatal(err)
	}
	// 我们发出的一封。
	if _, err := f.pool.Exec(ctx, `INSERT INTO email_messages
		(tenant_id, message_key, sender_id, sender_name, from_email, to_email,
		 subject, body, body_format, thread_key, status, queued_at, sent_at)
		VALUES ($1, gen_random_uuid(), $2, '我', 'me@263.net', 'ana@buyer.example',
		        '回复：报价', '好的', 'TEXT', $3, 'ACCEPTED', now(), now())`,
		f.tenantID, f.me, thread); err != nil {
		t.Fatal(err)
	}

	items, err := f.svc.GetMailThread(ctx, f.tenantID, f.me, 0, thread)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("会话里应该是两封，拿到 %d 封", len(items))
	}

	byDir := map[string]ThreadItem{}
	for _, it := range items {
		byDir[it.Direction] = it
	}

	in, out := byDir["IN"], byDir["OUT"]
	if in.FromEmail != "ana@buyer.example" {
		t.Errorf("收到的那封，发件人应该是客户，拿到 %q", in.FromEmail)
	}
	if in.FromName != "Ana Costa" {
		t.Errorf("收到的那封，发件人名字丢了：%q", in.FromName)
	}
	// 整段，不是第一个：抄了同事的那封信要看得出同事也在收件人里。
	if !strings.Contains(in.ToAll, "colleague@263.net") {
		t.Errorf("收到的那封只给了第一个收件人：%q", in.ToAll)
	}
	if in.Cc != "boss@buyer.example" {
		t.Errorf("收到的那封抄送丢了：%q", in.Cc)
	}

	if out.FromEmail != "me@263.net" {
		t.Errorf("发出的那封，发件人应该是我们自己，拿到 %q", out.FromEmail)
	}
	if out.ToAll != "ana@buyer.example" {
		t.Errorf("发出的那封，收件人不对：%q", out.ToAll)
	}

	// 关键的一条：两腿的 FromEmail 都是「写这封信的人」，所以它们必然不同。
	// 如果哪天有人把某一腿填反了，这里会变成相等。
	if in.FromEmail == out.FromEmail {
		t.Errorf("两个方向的发件人一样（%q），说明有一腿填的不是发件人", in.FromEmail)
	}
}

// 「发完信自己留不留副本」这个开关，决定的是我们会不会往服务器上塞一份。
//
// 生产上的毛病：263 把「保存客户端发信」做成了每个信箱各自的后台开关，
// yy@aaaindustryinc.com 开着、erptest@263.net 关着——同一个 smtp.263.net。
// 我们按主机名猜，于是前者每发一封，客户真实的邮箱里就多一封一模一样的信
// （36 发 36 重）。开关把这件事交给用户。
func TestTheSentCopySwitchDecidesWhetherWeAppend(t *testing.T) {
	no := false
	yes := true
	cases := []struct {
		name string
		acct MailAccount
		want bool
	}{
		// 没人表过态 → 按主机猜，也就是今天的行为。这两条钉的是「装上开关
		// 之后，不动它的信箱一封都不会变」。
		{"没表态的 263，照旧我们存", MailAccount{Host: "smtp.263.net"}, true},
		{"没表态的 Gmail，照旧不存", MailAccount{Host: "smtp.gmail.com"}, false},
		// 表过态就听用户的，主机名不再有发言权。
		{"263 上关掉（服务器自己会存）", MailAccount{Host: "smtp.263.net", KeepSentCopy: &no}, false},
		{"Gmail 上打开", MailAccount{Host: "smtp.gmail.com", KeepSentCopy: &yes}, true},
	}
	for _, c := range cases {
		if got := c.acct.ShouldKeepSentCopy(); got != c.want {
			t.Errorf("%s：拿到 %v，想要 %v", c.name, got, c.want)
		}
	}
}

// 开关只能改自己名下的信箱。
func TestTheSentCopySwitchOnlyReachesYourOwnMailbox(t *testing.T) {
	f := newFolderFixture(t, 9611)
	ctx := context.Background()

	if err := f.svc.SetKeepSentCopy(ctx, f.tenantID, f.me, f.account, false); err != nil {
		t.Fatalf("关掉自己的信箱失败：%v", err)
	}
	acct, err := f.svc.ForAccount(ctx, f.tenantID, f.account)
	if err != nil {
		t.Fatal(err)
	}
	if acct.ShouldKeepSentCopy() {
		t.Error("关掉了，发信时却仍然会自己塞一份")
	}

	// 别人的信箱：SQL 的 WHERE 判不到，影响零行，翻成 404 而不是静默成功。
	err = f.svc.SetKeepSentCopy(ctx, f.tenantID, f.me+1, f.account, true)
	if err == nil {
		t.Fatal("改了不属于这个人的信箱，而且没报错")
	}
	if !strings.Contains(err.Error(), "不在你名下") {
		t.Errorf("报的不是「不在你名下」：%v", err)
	}
}

// 合并发送的信，会话里要显示**全部**收件人和抄送。
//
// 报上来的样子：客户在 Gmail 里看到「to erptest, Fangchen」，我们自己的会话
// 里只有 erptest 一个，抄送那一行是空的。原因是 email_messages.to_email 只
// 存了第一个人——合并发送一行代表好几个人，完整名单在 email_message_recipients
// 上，而那张表以前没人读。
func TestAMergedSendShowsEveryRecipientAndCc(t *testing.T) {
	f := newFolderFixture(t, 9811)
	ctx := context.Background()
	const thread = "merged-cast"

	var msgID int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO email_messages
		(tenant_id, message_key, sender_id, sender_name, from_email, to_email,
		 subject, body, body_format, thread_key, send_mode, status, queued_at, sent_at)
		VALUES ($1, gen_random_uuid(), $2, '我', 'me@263.net', 'ana@buyer.example',
		        '报价', '正文', 'TEXT', $3, 'MERGED', 'ACCEPTED', now(), now())
		RETURNING id`, f.tenantID, f.me, thread).Scan(&msgID); err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct{ kind, email, name string }{
		{"TO", "ana@buyer.example", ""},
		{"TO", "bob@buyer.example", "Bob"},
		{"CC", "boss@buyer.example", "Boss"},
	} {
		if _, err := f.pool.Exec(ctx, `INSERT INTO email_message_recipients
			(tenant_id, message_id, kind, email, name) VALUES ($1,$2,$3,$4,$5)`,
			f.tenantID, msgID, r.kind, r.email, r.name); err != nil {
			t.Fatal(err)
		}
	}

	items, err := f.svc.GetMailThread(ctx, f.tenantID, f.me, 0, thread)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("应该只有一封，拿到 %d 封", len(items))
	}
	it := items[0]

	if !strings.Contains(it.ToAll, "ana@buyer.example") ||
		!strings.Contains(it.ToAll, "bob@buyer.example") {
		t.Errorf("收件人不全，只照着 to_email 显示了：%q", it.ToAll)
	}
	if !strings.Contains(it.Cc, "boss@buyer.example") {
		t.Errorf("抄送丢了：%q", it.Cc)
	}
	// 名字有就带上，和 Gmail 一样显示成人而不是一串地址。
	if !strings.Contains(it.ToAll, "Bob") {
		t.Errorf("有名字却没显示：%q", it.ToAll)
	}
}

// 分别发送的信没有明细行（一行本来就只对一个人），必须退回 to_email。
//
// 生产上 78 封分别发送的信明细行为 0——没有这条退路的话，它们的收件人会
// 整个变成空白。
func TestASeparateSendFallsBackToItsOwnRecipient(t *testing.T) {
	f := newFolderFixture(t, 9812)
	ctx := context.Background()
	const thread = "separate-one"

	if _, err := f.pool.Exec(ctx, `INSERT INTO email_messages
		(tenant_id, message_key, sender_id, sender_name, from_email, to_email,
		 subject, body, body_format, thread_key, send_mode, status, queued_at, sent_at)
		VALUES ($1, gen_random_uuid(), $2, '我', 'me@263.net', 'solo@buyer.example',
		        '报价', '正文', 'TEXT', $3, 'SEPARATE', 'ACCEPTED', now(), now())`,
		f.tenantID, f.me, thread); err != nil {
		t.Fatal(err)
	}

	items, err := f.svc.GetMailThread(ctx, f.tenantID, f.me, 0, thread)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("应该只有一封，拿到 %d 封", len(items))
	}
	if items[0].ToAll != "solo@buyer.example" {
		t.Errorf("没有明细行时收件人应该退回 to_email，拿到 %q", items[0].ToAll)
	}
	if items[0].Cc != "" {
		t.Errorf("没有抄送却给出了 %q", items[0].Cc)
	}
}

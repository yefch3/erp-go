package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func att(name string, mb float64) Attachment {
	return Attachment{FileName: name, FileSize: int64(mb * float64(1<<20))}
}

func fileNames(files []Attachment) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.FileName)
	}
	return out
}

func eq(t *testing.T, label string, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s = %v，想要 %v", label, got, want)
	}
}

// 装得下就全带，一个链接都不生成。
//
// 这条是这个功能最要紧的一条：绝大多数邮件的附件很小，它们的行为必须和今天
// 逐位相同。转链接对收件人是**看得见的变化**（拿到的是地址不是文件，有些企业
// 网关还会拦外链），不该为一封 2 MB 的信付这个代价。
func TestSmallAttachmentsAllRideAlong(t *testing.T) {
	files := []Attachment{att("报价.pdf", 2), att("图纸.dwg", 3)}
	carried, linked := splitCarriedAndLinked(files, MaxCarriedAttachmentBytes)
	eq(t, "随信带", fileNames(carried), "报价.pdf", "图纸.dwg")
	if len(linked) != 0 {
		t.Errorf("装得下却生成了链接：%v", fileNames(linked))
	}
}

// 装不下就从**大的**开始转，直到剩下的装得下。
//
// 挑大的转有两个好处：一次腾出最多空间，所以被迫转链接的文件数最少；而且
// 解释起来只有一句话，跟人手工做同一件事的做法一致。
func TestTheBiggestOnesBecomeLinksUntilTheRestFits(t *testing.T) {
	// 合计 22 MB，超过 15 MB。转掉最大的那个 12 MB 之后剩 10 MB，装得下。
	files := []Attachment{att("小.pdf", 4), att("大.zip", 12), att("中.xlsx", 6)}
	carried, linked := splitCarriedAndLinked(files, MaxCarriedAttachmentBytes)
	eq(t, "随信带", fileNames(carried), "小.pdf", "中.xlsx")
	eq(t, "转链接", fileNames(linked), "大.zip")
}

// 顺序保持原样——写信框要照着标出哪些会变成链接，顺序变了人就对不上号。
func TestBothSidesKeepTheOriginalOrder(t *testing.T) {
	// 合计 25 MB。转掉 a（9）还剩 16，仍然超；再转 c（9）剩 7，装得下。
	// 两堆各有两个，正好能看出顺序有没有被打乱。
	files := []Attachment{att("a", 9), att("b", 1), att("c", 9), att("d", 6)}
	carried, linked := splitCarriedAndLinked(files, MaxCarriedAttachmentBytes)
	eq(t, "随信带", fileNames(carried), "b", "d")
	eq(t, "转链接", fileNames(linked), "a", "c")
}

// 一个文件自己就超过分界线：它一定走链接，而且不该把别的也拖下水。
func TestOneHugeFileGoesAloneAndDoesNotDragTheRestWithIt(t *testing.T) {
	files := []Attachment{att("巨无霸.zip", 60), att("说明.pdf", 1)}
	carried, linked := splitCarriedAndLinked(files, MaxCarriedAttachmentBytes)
	eq(t, "随信带", fileNames(carried), "说明.pdf")
	eq(t, "转链接", fileNames(linked), "巨无霸.zip")
}

// 同样大小的按原顺序，不能今天转这个明天转那个。
func TestEquallySizedFilesSplitDeterministically(t *testing.T) {
	files := []Attachment{att("一", 10), att("二", 10)}
	for i := 0; i < 5; i++ {
		carried, linked := splitCarriedAndLinked(files, MaxCarriedAttachmentBytes)
		eq(t, "随信带", fileNames(carried), "二")
		eq(t, "转链接", fileNames(linked), "一")
	}
}

// 正好卡在分界线上算装得下：这条线是「超过才转」。
func TestExactlyAtTheLimitStillRidesAlong(t *testing.T) {
	files := []Attachment{{FileName: "刚好", FileSize: MaxCarriedAttachmentBytes}}
	_, linked := splitCarriedAndLinked(files, MaxCarriedAttachmentBytes)
	if len(linked) != 0 {
		t.Errorf("正好等于上限被转成了链接：%v", fileNames(linked))
	}
}

// 链接块要明写出来，而且地址和文件名都要转义——文件名是用户起的，里面
// 可以有引号和尖括号，直接拼进 HTML 就是把写信的人变成了注入的入口。
func TestTheLinkBlockIsAppendedAndEscaped(t *testing.T) {
	linked := []Attachment{{
		FileName:    `报价"<script>.pdf`,
		FileSize:    30 << 20,
		DownloadURL: "https://erp.example/api/public/mail-files/abc",
	}}
	out := AppendBigFileLinks("<p>见附件</p>", "HTML", linked)

	if !strings.Contains(out, "<p>见附件</p>") {
		t.Error("原来的正文没了")
	}
	if !strings.Contains(out, "https://erp.example/api/public/mail-files/abc") {
		t.Errorf("链接没接上：\n%s", out)
	}
	if !strings.Contains(out, "30.0 MB") {
		t.Errorf("没告诉收件人这个文件多大：\n%s", out)
	}
	if strings.Contains(out, "<script>") {
		t.Errorf("文件名没转义，正文里出现了未转义的标签：\n%s", out)
	}
}

// 纯文本的信不能塞 HTML 进去。
func TestThePlainTextLinkBlockCarriesNoMarkup(t *testing.T) {
	linked := []Attachment{{FileName: "大.zip", FileSize: 30 << 20, DownloadURL: "https://x/y"}}
	out := AppendBigFileLinks("见附件", "TEXT", linked)
	if strings.Contains(out, "<") {
		t.Errorf("纯文本正文里出现了标签：\n%s", out)
	}
	if !strings.Contains(out, "https://x/y") {
		t.Errorf("链接没接上：\n%s", out)
	}
}

// 没有要转的文件时，正文一个字节都不能动。
func TestNothingIsAppendedWhenNothingWasLinked(t *testing.T) {
	const body = "<p>就这些</p>"
	if got := AppendBigFileLinks(body, "HTML", nil); got != body {
		t.Errorf("没有链接却改了正文：%q", got)
	}
}

// 取件口只发一次，重发同一封信不换地址。
//
// 重要在于：一封信失败重试时如果每次换一个 token，先收到的那个人手上的链接
// 就废了，而他不会知道为什么——链接还在，点开是 404。
func TestTheDownloadTokenIsMintedOnceAndReused(t *testing.T) {
	f := newFolderFixture(t, 9711)
	ctx := context.Background()
	attID := f.insertOutboundAttachment(t, "大文件.zip", 30<<20)

	first, failed := f.svc.linkableAttachments(ctx, f.tenantID,
		[]Attachment{{ID: attID, FileName: "大文件.zip", FileSize: 30 << 20}},
		"https://erp.example")
	if len(failed) != 0 || len(first) != 1 {
		t.Fatalf("没能配上取件口：%d 成功 / %d 失败", len(first), len(failed))
	}
	if !strings.Contains(first[0].DownloadURL, "/api/public/mail-files/") {
		t.Errorf("地址不对：%s", first[0].DownloadURL)
	}

	second, _ := f.svc.linkableAttachments(ctx, f.tenantID,
		[]Attachment{{ID: attID, FileName: "大文件.zip", FileSize: 30 << 20}},
		"https://erp.example")
	if second[0].DownloadURL != first[0].DownloadURL {
		t.Errorf("重发换了地址，先收到的人手上那条就废了：\n%s\n%s",
			first[0].DownloadURL, second[0].DownloadURL)
	}
}

// 撤回之后立刻失效，而且失效和「没这个 token」长得一模一样。
func TestAWithdrawnLinkStopsResolvingAndLooksLikeNothing(t *testing.T) {
	f := newFolderFixture(t, 9712)
	ctx := context.Background()
	f.svc.files = &presigningFiles{}
	attID := f.insertOutboundAttachment(t, "报价.pdf", 30<<20)

	got, _ := f.svc.linkableAttachments(ctx, f.tenantID,
		[]Attachment{{ID: attID, FileName: "报价.pdf", FileSize: 30 << 20}},
		"https://erp.example")
	token := got[0].DownloadURL[strings.LastIndex(got[0].DownloadURL, "/")+1:]

	if _, _, err := f.svc.OpenAttachmentLink(ctx, token); err != nil {
		t.Fatalf("撤回之前应该取得到：%v", err)
	}
	if err := f.svc.WithdrawAttachmentLink(ctx, f.tenantID, attID); err != nil {
		t.Fatalf("撤回失败：%v", err)
	}

	_, _, err := f.svc.OpenAttachmentLink(ctx, token)
	if err == nil {
		t.Fatal("撤回之后链接还能用")
	}
	// 和一个根本不存在的 token 给出同样的答案：分开说等于告诉试探的人
	// 哪些 token 存在过。
	_, _, missing := f.svc.OpenAttachmentLink(ctx, "no-such-token-at-all")
	if err.Error() != missing.Error() {
		t.Errorf("撤回和不存在给的答案不一样，可以据此试探：\n撤回：%v\n不存在：%v", err, missing)
	}
}

// insertOutboundAttachment 插一个发信附件（浏览器直传之后登记的那种）。
func (f *folderFixture) insertOutboundAttachment(t *testing.T, name string, size int64) int64 {
	t.Helper()
	var campaignID int64
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO email_campaigns (tenant_id, campaign_no, subject_tpl, body_tpl, sender_id)
		 VALUES ($1, $2, '主题', '正文', 1) RETURNING id`,
		f.tenantID, fmt.Sprintf("C-TEST-%d", f.tenantID)).Scan(&campaignID); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO email_attachments
		 (tenant_id, campaign_id, file_name, file_key, file_size, content_type)
		 VALUES ($1, $2, $3, $4, $5, 'application/zip') RETURNING id`,
		f.tenantID, campaignID, name, "mail/out/"+name, size).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// presigningFiles 只回答「这个 key 的地址是什么」。
//
// 不复用 readRecordingFiles：那个把 Files 接口空着嵌进来，只覆盖了 Get，
// 一调 PresignGet 就是对着 nil 接口取方法——直接段错误。
type presigningFiles struct{ Files }

func (presigningFiles) PresignGet(_ context.Context, key, saveAs string) (string, error) {
	return "https://storage.example/" + key + "?saveAs=" + saveAs, nil
}

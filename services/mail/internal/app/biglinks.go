package app

import (
	"context"
	"fmt"
	"html"
	"sort"
	"strings"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 公开路由上的统一答案。不区分「没这个 token」「被撤回了」「文件没了」——
// 对着一个公开地址把这三种分开说，等于告诉试探的人哪些 token 存在过。
var errAttachmentLinkGone = apierr.NotFound("NT_ATTACHMENT_LINK_NOT_FOUND", "链接不存在或已失效")

// 超大附件：装不下的文件改成下载链接。
//
// 邮件能带多大的附件，不是我们说了算。附件必须 base64 编码才能走 SMTP，
// **体积涨三分之一**；而真正卡住的是**收件人那一侧**的上限——Gmail 是 25 MB
// 编码后的整封信，也就是实际文件约 18 MB 封顶，而客户用什么邮箱是客户的事。
//
// 所以「把上限调高」解决不了任何问题：调高只是把「写信时当场拒绝」换成
// 「发出去之后按收件人退信」，而后者是异步的、一个收件人一封、用户以为
// 自己已经发出去了。把一个确定的、当场的拒绝换成一个不确定的、事后的失败，
// 是往坏里改。
//
// 每个邮件客户端都是同一个做法，没有一家靠邮件协议解决，全都退回自己家的
// 云盘：Outlook → OneDrive，Foxmail → 腾讯中转站，Gmail 网页版 → Drive。
// 我们的对象存储本来就有，而且发信附件现在就是浏览器直传上去的——文件已经
// 在该在的地方，缺的只是一个公开取件口。

const (
	// MaxCarriedAttachmentBytes 是「一封信随身带多少附件」的分界，超过的转
	// 成链接。
	//
	// 15 MB 编码后约 20 MB，对 Gmail 的 25 MB、Outlook、263 都留了余量。
	//
	// **它比原来的 20 MB 更小，这是有意的。** 20 MB 那条线的含义是「我们敢
	// 让你发的最大值，超了就拒绝」——所以要定高，免得挡人；而它编码后是
	// 27 MB，发给 Gmail 客户其实已经会退信了，只是很少有人真塞满。
	//
	// 现在这条线的含义变了：超过它没有人被拦住，只是换一种送达方式。所以
	// 它该定在**可靠送达**的位置，而不是「最多敢发多少」。往下调，送达率
	// 反而变高。
	MaxCarriedAttachmentBytes = 15 << 20
)

// splitCarriedAndLinked 把一封信的附件分成随信带的和转链接的两堆。
//
// 规则：装得下就全带；装不下就**从大的开始**转链接，直到剩下的装得下。
//
// 挑大的转，是因为这是人手工做同一件事时的做法，解释起来只有一句话；而且
// 一次就能腾出最多的空间，被迫转链接的文件数最少。相比之下「按顺序装、装
// 不下的跳过」会出现「14 MB 带上了、2 MB 转了链接、后面 1 MB 又带上了」这
// 种没人能预料的结果。
//
// 两个返回值都保持原来的顺序（也就是用户在写信框里看到的顺序），只是分成了
// 两堆——写信框要照着它标出哪些会变成链接，而顺序变了人就对不上号。
func splitCarriedAndLinked(files []Attachment, limit int64) (carried, linked []Attachment) {
	total := int64(0)
	for _, f := range files {
		total += f.FileSize
	}
	if total <= limit {
		return files, nil
	}

	// 按大小从大到小挑，同样大的按原顺序——排序要稳定，否则同样两个文件
	// 今天转这个、明天转那个。
	order := make([]int, len(files))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return files[order[a]].FileSize > files[order[b]].FileSize
	})

	link := make([]bool, len(files))
	for _, i := range order {
		if total <= limit {
			break
		}
		link[i] = true
		total -= files[i].FileSize
	}

	for i, f := range files {
		if link[i] {
			linked = append(linked, f)
		} else {
			carried = append(carried, f)
		}
	}
	return carried, linked
}

// linkableAttachments 给要转成链接的附件各配一个公开取件口，返回带地址的那份。
//
// token 只在这里才发：绝大多数附件永远不需要一个公开地址，提前给每个都开一个
// 是白白扩大暴露面。已经有 token 的复用——同一封信重试时不该每次换一个地址，
// 否则先收到的人手上那条就废了。
//
// 配不上的（存储读不到、库写不进）**原样退回去随信带**：宁可这封信因为太大
// 被服务器拒收——那是个当场看得见的错误——也不要发出去一个点开是 404 的链接。
func (s *Service) linkableAttachments(
	ctx context.Context, tenantID int64, files []Attachment, baseURL string,
) (linked []Attachment, failed []Attachment) {
	for _, f := range files {
		tok, err := s.q.EnsureAttachmentToken(ctx, store.EnsureAttachmentTokenParams{
			TenantID: tenantID, ID: f.ID, Token: mustToken(),
		})
		if err != nil || tok == "" {
			s.log.Warn("could not mint a download link for a large attachment",
				"file", f.FileName, "id", f.ID, "err", err)
			failed = append(failed, f)
			continue
		}
		f.DownloadURL = strings.TrimRight(baseURL, "/") + "/api/public/mail-files/" + tok
		linked = append(linked, f)
	}
	return linked, failed
}

// mustToken 出不了错的随机 token：randomToken 只在系统熵源坏掉时报错，那时
// 发信本来也进行不下去。返回空串会被上面当成失败处理。
func mustToken() string {
	t, err := randomToken()
	if err != nil {
		return ""
	}
	return t
}

// AppendBigFileLinks 把「这几个文件太大，改成下载」那一段接在正文后面。
//
// 明写出来而不是悄悄换掉，理由和写信框里要标出来是同一个：收件人拿到的东西
// 变了——不是附件而是链接，有些企业安全网关会拦外链，文件也存在我们服务器
// 上。Outlook 和 Gmail 同样会当面说清楚。
func AppendBigFileLinks(body, format string, linked []Attachment) string {
	if len(linked) == 0 {
		return body
	}
	if format != "HTML" {
		var b strings.Builder
		b.WriteString(body)
		b.WriteString("\n\n--\n以下文件较大，改用下载链接（长期有效）：\n")
		for _, f := range linked {
			fmt.Fprintf(&b, "%s（%s）\n%s\n", f.FileName, humanSize(f.FileSize), f.DownloadURL)
		}
		return b.String()
	}
	var b strings.Builder
	b.WriteString(body)
	b.WriteString(`<div style="margin-top:16px;padding:12px 14px;border:1px solid #e4e7ed;` +
		`border-radius:8px;background:#fafafa;font-size:13px">`)
	b.WriteString(`<div style="color:#909399;margin-bottom:8px">以下文件较大，改用下载链接（长期有效）：</div>`)
	for _, f := range linked {
		fmt.Fprintf(&b,
			`<div style="margin:4px 0"><a href="%s" style="color:#409eff">%s</a>`+
				`<span style="color:#909399"> · %s</span></div>`,
			html.EscapeString(f.DownloadURL), html.EscapeString(f.FileName),
			html.EscapeString(humanSize(f.FileSize)))
	}
	b.WriteString(`</div>`)
	return b.String()
}

// humanSize 是给收件人看的大小。不复用前端那份：这段字要进邮件正文，
// 收件人那边没有我们的 JavaScript。
func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// OpenAttachmentLink 把公开 token 换成一条短期有效的存储地址。
//
// **不把文件读进内存**，这是和 OpenImage 唯一的、也是必须的区别：图片有 2 MB
// 的硬上限，整个读出来无所谓；走这条路的偏偏是大文件，一个几百 MB 的下载
// 穿过我们的进程，一个人点两下就能把内存吃光。所以这里只负责回答「它在哪」，
// 网关拿着答案让浏览器直接去存储取。
//
// 查不到就是查不到，不区分「没这个 token」「被撤回了」「文件没了」——对着
// 一个公开地址把这三种分开说，等于告诉试探的人哪些 token 存在过。
func (s *Service) OpenAttachmentLink(ctx context.Context, token string) (url, fileName string, err error) {
	if s.files == nil || strings.TrimSpace(token) == "" {
		return "", "", errAttachmentLinkGone
	}
	row, err := s.q.ResolveAttachmentToken(ctx, token)
	if err != nil {
		return "", "", errAttachmentLinkGone
	}
	// saveAs：浏览器保存时用原来的文件名，而不是存储里那串 key。
	signed, err := s.files.PresignGet(ctx, row.FileKey, row.FileName)
	if err != nil {
		s.log.Warn("could not sign a large-attachment download", "file", row.FileName, "err", err)
		return "", "", errAttachmentLinkGone
	}
	return signed, row.FileName, nil
}

// WithdrawAttachmentLink 撤回一条已经发出去的下载链接。
//
// 行留着，只是不再服务——客户问「你发我的链接打不开」时答得上来是被撤回了，
// 而不是一句查无此物。同 email_images 的撤回。
//
// 已经送达的邮件正文改不了，那条链接还在客户手里，只是从此点开是 404。这跟
// 「撤回一封已读的邮件」是同一类事：能停掉的是我们这一侧。
func (s *Service) WithdrawAttachmentLink(ctx context.Context, tenantID, id int64) error {
	n, err := s.q.WithdrawAttachmentLink(ctx, store.WithdrawAttachmentLinkParams{
		TenantID: tenantID, ID: id,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return apierr.NotFound("NT_ATTACHMENT_LINK_NOT_FOUND", "这个附件没有对外的下载链接")
	}
	return nil
}

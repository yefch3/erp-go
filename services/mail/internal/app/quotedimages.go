package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 引用里那张图怎么活下来。
//
// 回复一封带内嵌图片的信时，写信框把阅读视图里那段 HTML 原样塞进 blockquote。
// 阅读视图里的 <img src> 是一条**限时**的对象存储地址——短期有效对「现在打开
// 来看」是对的，那是别人发来的私有附件，本就不该有一个长期地址。问题是它被
// 抄进正文之后就跟着信发了出去，而地址会过期：客户过一阵打开看到裂开的图标，
// 我们自己回头看已发送也一样。
//
// 生产上那封（email_messages 78）正文里就是这么一条 S3 签名地址，截图上裂的
// 就是它；同一封信底部的 ERP 签名图好好的，因为签名走的是另一条路。
//
// 修法就是签名图那条路：让图片**跟着信走**，成为消息自己的一部分（cid:），
// 而不是一个收件人还得回来取的地址。理由见 InlineMailImages 上面那一段，那里
// 已经把「为什么内嵌比链接强」写清楚了，这里只是把同一个待遇给引用里的图。
//
// **正文不是权威。** <img src> 是人能手打的东西，照着它去打开一个对象，等于
// 让外面的人指挥我们读存储里的任意一个 key。所以地址里的 key 只当成一个
// *问题*：拿去问 AttachmentsByKeys，问的范围是这个人自己名下的信，只有问回来
// 的才会被打开。正文说了不算，库说了算。

// imgSrc 取出每个 <img> 的地址。只认双引号，因为经过 SanitizeForReading 的正文
// 属性一律是双引号的，而没经过的那些是我们自己的写信框生成的，也一样。
var imgSrc = regexp.MustCompile(`(?i)<img[^>]+src="([^"]+)"`)

// ourStorageKey 是这条地址在我们自己存储里对应的 key，不是我们的就回空。
//
// 按 key 自己的形状认，不按主机名认：同一个桶在不同写法下有好几个主机名
// （虚拟主机式、路径式、外面再套一层代理），列主机名的话每换一次就得改一次。
// 而这个服务写进存储的 key 一律以 "mail/" 开头（见 objectKey 和 ingest 里拼
// 附件 key 的那行），既稳定又好认。
//
// 认错了也不要紧：客户自己网站上一条 .../img/mail/logo.png 会被当成候选，然后
// 在库里问不到，于是原样留着。真正的闸在库那一步，不在这里。
func ourStorageKey(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Path == "" {
		return ""
	}
	p := strings.TrimPrefix(u.Path, "/")
	i := strings.Index(p, "mail/")
	if i < 0 {
		return ""
	}
	return p[i:]
}

// quotedImageContentID 是这张图在这封信里的 cid。
//
// 从 key 算出来而不是随机生成：同一张图在正文里出现两次就该是同一个 cid，
// 带一份、引两次。随机的话一封信里会出现同一张图的两个副本。
func quotedImageContentID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "q" + hex.EncodeToString(sum[:12])
}

// inlineQuotedStorageImages 把正文借用的、我们自己存储里的图片变成随信携带的部件。
func (s *Service) inlineQuotedStorageImages(
	ctx context.Context, tenantID, ownerID int64, html string,
) (string, []InlineImage) {
	if s.files == nil || tenantID == 0 || ownerID == 0 || html == "" {
		return html, nil
	}

	// 一、正文点到了哪些看起来像我们存储的 key。去重：一张图引两次问一次。
	seen := map[string]bool{}
	var candidates []string
	for _, m := range imgSrc.FindAllStringSubmatch(html, -1) {
		key := ourStorageKey(m[1])
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		candidates = append(candidates, key)
	}
	if len(candidates) == 0 {
		return html, nil
	}

	// 二、这些 key 里哪些真是这个人自己的附件。正文说了不算的那一步。
	rows, err := s.q.AttachmentsByKeys(ctx, store.AttachmentsByKeysParams{
		TenantID: tenantID, OwnerID: ownerID, FileKeys: candidates,
	})
	if err != nil {
		// 问不到就别带。发信不能因为一张引用里的图而失败——这跟
		// InlineMailImages 里「一张图取不到就当链接发」是同一个取舍。
		s.log.Warn("could not check which quoted pictures may travel with the mail",
			"candidates", len(candidates), "err", err)
		return html, nil
	}

	// 三、读出来，做成部件。
	carried := map[string]string{} // key → cid
	var out []InlineImage
	for _, r := range rows {
		if !cacheableTypes[strings.ToLower(r.ContentType)] {
			continue
		}
		rc, err := s.files.Get(ctx, r.FileKey)
		if err != nil {
			s.log.Warn("a quoted picture could not be read, leaving it as a link",
				"key", r.FileKey, "err", err)
			continue
		}
		data, err := io.ReadAll(io.LimitReader(rc, MaxImageBytes+1))
		rc.Close()
		if err != nil || len(data) == 0 || int64(len(data)) > MaxImageBytes {
			s.log.Warn("a quoted picture was unreadable or oversized, leaving it as a link",
				"key", r.FileKey, "bytes", len(data))
			continue
		}
		cid := quotedImageContentID(r.FileKey)
		out = append(out, InlineImage{
			ContentID:   cid,
			FileName:    "image" + extensionFor(strings.ToLower(r.ContentType)),
			ContentType: r.ContentType,
			Data:        data,
		})
		carried[r.FileKey] = cid
	}
	if len(carried) == 0 {
		return html, nil
	}

	// 四、只改真的带上了的那几个。指向一个不存在的部件的 cid: 比原来那条链接
	// 更糟——链接至少还有一次能打开的机会。同 InlineMailImages。
	rewritten := imgSrc.ReplaceAllStringFunc(html, func(tag string) string {
		m := imgSrc.FindStringSubmatch(tag)
		if m == nil {
			return tag
		}
		cid, ok := carried[ourStorageKey(m[1])]
		if !ok {
			return tag
		}
		return strings.Replace(tag, `src="`+m[1]+`"`, `src="cid:`+cid+`"`, 1)
	})
	return rewritten, out
}

// refreshStorageImageLinks 把正文里指向我们存储的、已经过期的地址换成刚签的。
//
// 给「回头看自己发出去的信」用。存下来的正文是当时写的那一份（有意如此，见
// worker.go 里那句「什么被存下来就是人写了什么」），里面那条签名地址早过期了，
// 于是会话里我们自己那几条的图片全是裂的。
//
// fresh 的 key 是对象 key，来自这条会话自己的附件行——不是照着正文里的地址去
// 签。这个区别就是安全边界：正文里的地址是人能手打的，照着它签等于替外面的人
// 签任意一个 key；而这条会话有哪些附件，是库说的。
//
// 换不到的原样留着。一条打不开的链接不比一个空 src 差，而猜错了会把别人网站上
// 的图也改掉。
func refreshStorageImageLinks(html string, fresh map[string]string) string {
	if html == "" || len(fresh) == 0 {
		return html
	}
	return imgSrc.ReplaceAllStringFunc(html, func(tag string) string {
		m := imgSrc.FindStringSubmatch(tag)
		if m == nil {
			return tag
		}
		key := ourStorageKey(m[1])
		if key == "" {
			return tag
		}
		u, ok := fresh[key]
		if !ok {
			return tag
		}
		return strings.Replace(tag, `src="`+m[1]+`"`, `src="`+u+`"`, 1)
	})
}

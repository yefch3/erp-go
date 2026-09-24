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

// 引用里的图一封信最多带多少。按 2026-09 发出的 26 封回信实测，每封带的图中位数
// 80 KB、最多 165 KB，这两个数碰不到；它们防的是回一封几十张图的广告信，一下子
// 把信撑到收件方拒收（MaxCampaignBytes 那边说的 25 MB 那堵墙）。
const (
	quotedImagesMaxCount = 30
	quotedImagesMaxBytes = 5 << 20
)

// inlineQuotedStorageImages 把正文借用的、我们自己存储里的图片变成随信携带的部件。
//
// 三种图走这里：原信的附件图（cid）、正文自带的图（data:，见 dataimages.go）、
// 以及从发件人网站缓存下来的副本。第三种原来不走这里，于是回信里发出去的是一条
// 一小时后过期的存储地址，客户打开就是裂图（2026-09 发出的 289 封里有 26 封）。
// 追踪像素、排版用的一像素占位图一律照带：带在信里，它只是一张几十字节的图，
// 不会替任何人报告「已读」，排版也不会塌。
//
// 超出上面那两个数的，缓存副本改回发件人的原地址（至少不会过期），附件图和
// 自带图没有原地址可退，照旧留着那条链接。按正文里出现的先后带：靠前的图离
// 回信的正文最近。
func (s *Service) inlineQuotedStorageImages(
	ctx context.Context, tenantID, ownerID int64, html string,
) (string, []InlineImage) {
	if s.files == nil || tenantID == 0 || ownerID == 0 || html == "" {
		return html, nil
	}

	// 一、正文点到了哪些看起来像我们存储的 key。去重：一张图引两次问一次。
	candidates := storageKeysIn(html)
	if len(candidates) == 0 {
		return html, nil
	}

	// 二、这些 key 里哪些真是这个人自己的。正文说了不算的那一步。
	byKey, err := s.ownStorageImages(ctx, tenantID, ownerID, candidates)
	if err != nil {
		// 问不到就别带。发信不能因为一张引用里的图而失败——这跟
		// InlineMailImages 里「一张图取不到就当链接发」是同一个取舍。
		s.log.Warn("could not check which quoted pictures may travel with the mail",
			"candidates", len(candidates), "err", err)
		return html, nil
	}

	// 三、按出现的先后读出来，做成部件；超额的记下原地址。
	carried := map[string]string{}  // key → cid
	fallback := map[string]string{} // key → 发件人的原地址
	var out []InlineImage
	total := 0
	for _, key := range candidates {
		r, ok := byKey[key]
		if !ok || !cacheableTypes[strings.ToLower(r.ContentType)] {
			continue
		}
		if len(out) >= quotedImagesMaxCount || total >= quotedImagesMaxBytes {
			if u := senderAddress(r.SourceUrl); u != "" {
				fallback[key] = u
			}
			continue
		}
		data, ok := s.readQuotedImage(ctx, r.FileKey)
		if !ok {
			continue
		}
		if total+len(data) > quotedImagesMaxBytes {
			if u := senderAddress(r.SourceUrl); u != "" {
				fallback[key] = u
			}
			continue
		}
		total += len(data)
		cid := quotedImageContentID(r.FileKey)
		out = append(out, InlineImage{
			ContentID:   cid,
			FileName:    "image" + extensionFor(strings.ToLower(r.ContentType)),
			ContentType: r.ContentType,
			Data:        data,
		})
		carried[r.FileKey] = cid
	}
	if len(carried) == 0 && len(fallback) == 0 {
		return html, nil
	}

	// 四、只改真的带上了的、或者真有原地址可退的那几个。指向一个不存在的部件的
	// cid: 比原来那条链接更糟——链接至少还有一次能打开的机会。同 InlineMailImages。
	rewritten := imgSrc.ReplaceAllStringFunc(html, func(tag string) string {
		m := imgSrc.FindStringSubmatch(tag)
		if m == nil {
			return tag
		}
		key := ourStorageKey(m[1])
		if cid, ok := carried[key]; ok {
			return strings.Replace(tag, `src="`+m[1]+`"`, `src="cid:`+cid+`"`, 1)
		}
		if u, ok := fallback[key]; ok {
			return strings.Replace(tag, `src="`+m[1]+`"`, `src="`+u+`"`, 1)
		}
		return tag
	})
	return rewritten, out
}

// storageKeysIn lists, in order of appearance and without repeats, the keys of
// our own storage that the pictures in these bodies point at.
//
// 用 mapImageURLs 而不是只认双引号的 imgSrc：收到的原文不一定是我们的写信框
// 生成的，单引号的也有。
func storageKeysIn(bodies ...string) []string {
	seen := map[string]bool{}
	var keys []string
	for _, b := range bodies {
		mapImageURLs(b, func(raw string) string {
			if key := ourStorageKey(strings.TrimSpace(raw)); key != "" && !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
			return raw
		})
	}
	return keys
}

// ownStorageImages asks which of these keys belong to this person's own mail.
func (s *Service) ownStorageImages(ctx context.Context, tenantID, ownerID int64, keys []string) (map[string]store.AttachmentsByKeysRow, error) {
	rows, err := s.q.AttachmentsByKeys(ctx, store.AttachmentsByKeysParams{
		TenantID: tenantID, OwnerID: ownerID, FileKeys: keys,
	})
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]store.AttachmentsByKeysRow, len(rows))
	for _, r := range rows {
		if _, dup := byKey[r.FileKey]; !dup {
			byKey[r.FileKey] = r
		}
	}
	return byKey, nil
}

func (s *Service) readQuotedImage(ctx context.Context, key string) ([]byte, bool) {
	rc, err := s.files.Get(ctx, key)
	if err != nil {
		s.log.Warn("a quoted picture could not be read, leaving it as a link", "key", key, "err", err)
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(rc, MaxImageBytes+1))
	rc.Close()
	if err != nil || len(data) == 0 || int64(len(data)) > MaxImageBytes {
		s.log.Warn("a quoted picture was unreadable or oversized, leaving it as a link",
			"key", key, "bytes", len(data))
		return nil, false
	}
	return data, true
}

// senderAddress is where a cached picture originally came from, if that is an
// address worth pointing a recipient at; empty otherwise (an attachment or a
// picture carried in the body has none).
//
// 这条地址是原样写回正文的，而写回发生在净化之后，所以这里自己把关：只认
// http(s)，带引号、尖括号或空白的一律不要。
func senderAddress(source string) string {
	if !strings.HasPrefix(source, "https://") && !strings.HasPrefix(source, "http://") {
		return ""
	}
	if strings.ContainsAny(source, "\"'<> \t\r\n") {
		return ""
	}
	return source
}

// freshOwnStorageLinks signs afresh every picture in these bodies that points
// at our own storage and belongs to this person's mail.
//
// 给「回头看」用：我们发出去的信、收到的回信里引用的我们那条地址，都是当时
// 签的，一小时就过期。key 只是拿去问的问题，本人名下的信里真有它才签——
// 和 inlineQuotedStorageImages 同一道闸。
func (s *Service) freshOwnStorageLinks(ctx context.Context, tenantID, ownerID int64, bodies ...string) map[string]string {
	if s.files == nil {
		return nil
	}
	keys := storageKeysIn(bodies...)
	if len(keys) == 0 {
		return nil
	}
	byKey, err := s.ownStorageImages(ctx, tenantID, ownerID, keys)
	if err != nil {
		s.log.Warn("could not re-sign the pictures a body points at", "keys", len(keys), "err", err)
		return nil
	}
	fresh := make(map[string]string, len(byKey))
	for key, r := range byKey {
		if u := s.signImage(ctx, key, r.ContentType); u != "" {
			fresh[key] = u
		}
	}
	return fresh
}

// refreshStorageImageLinks 把正文里指向我们存储的、已经过期的地址换成刚签的。
//
// 给「回头看自己发出去的信」用。存下来的正文是当时写的那一份（有意如此，见
// worker.go 里那句「什么被存下来就是人写了什么」），里面那条签名地址早过期了，
// 于是会话里我们自己那几条的图片全是裂的。
//
// fresh 的 key 是对象 key，来自库里的行（这条会话的附件，或 freshOwnStorageLinks
// 问回来的本人名下的图）——不是照着正文里的地址去签。这个区别就是安全边界：
// 正文里的地址是人能手打的，照着它签等于替外面的人签任意一个 key；而这个人
// 名下有哪些图，是库说的。
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

// onlyKeysIn narrows a conversation's fresh links to the keys one body names
// itself, so that a picture swapped in from elsewhere is never overwritten.
func onlyKeysIn(fresh map[string]string, body string) map[string]string {
	if len(fresh) == 0 {
		return nil
	}
	var out map[string]string
	for _, k := range storageKeysIn(body) {
		if u, ok := fresh[k]; ok {
			if out == nil {
				out = map[string]string{}
			}
			out[k] = u
		}
	}
	return out
}

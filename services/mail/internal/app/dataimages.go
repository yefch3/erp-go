package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"maps"
	"strings"
)

// 正文里自带的图片：<img src="data:image/png;base64,iVBOR...">。
//
// Foxmail 把截图粘进正文就是这么写的——图不作为附件挂在信上，而是整张转成
// 文字写进 HTML，一张就是十几万个字符。2026-09-24 清点客户来信：128 封带这种
// 图，155 处，只有 31 张不同的图（回信把前面的图一轮一轮再带一遍）。原来它们
// 在我们这里一张都不显示：清理白名单只放行 http/https 的图片地址（那条是为了
// 挡 javascript:），缓存那一遍又把 data: 跳过了，于是只剩一个没有 src 的 <img>。
//
// **不直接放行 data:。** 生产上一个 11 封的会话里这种图加起来 8.5 MB，放行就是
// 打开会话时一次回 8.5 MB 的正文——国内那条线最怕的就是大响应。所以它走外链
// 图片那条路：缓存那一遍把它解出来存进对象存储，阅读时换成签名地址，像图片
// 附件一样另外加载。
//
// 库里那一行的 source_url 记的不是原文（十几万个字符），是 dataImageKey：原文
// 去掉空白以后的 sha256。阅读时拿正文里的原文算同一个 key 去对，所以存和读
// 必须用同一个函数。

// dataImageKeyPrefix 开头的 source_url 是正文自带的图，不是一个能访问的地址。
const dataImageKeyPrefix = "data:sha256,"

// dataImageKey is what the cache row records for a picture carried inside the
// body, and what the reader looks it up by. Empty for anything else.
//
// 空白去掉再算：长长的 base64 在正文里常被折成好几行，折法不同的同一张图
// 应该是同一张。
func dataImageKey(raw string) string {
	if !isDataURL(raw) {
		return ""
	}
	sum := sha256.Sum256([]byte(stripASCIISpace(raw)))
	return dataImageKeyPrefix + hex.EncodeToString(sum[:])
}

func isDataURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	return len(raw) >= 5 && strings.EqualFold(raw[:5], "data:")
}

func stripASCIISpace(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r', '\f':
			return -1
		}
		return r
	}, s)
}

// decodeDataImage reads the picture out of a data: address.
//
// 只收 base64 编码、自称是图片的；类型最后**看字节**，不看它自己怎么说——和
// 外链图片同一条规矩（见 cacheableTypes）：存下来的东西会带着这里记的类型
// 交给浏览器，一段自称 image/png 的 HTML 不能混进去。百分号编码的那种几乎只见
// 于 SVG，而 SVG 本来就不在可存的类型里。
func decodeDataImage(raw string) ([]byte, string, bool) {
	s := stripASCIISpace(raw)
	if !isDataURL(s) {
		return nil, "", false
	}
	comma := strings.IndexByte(s, ',')
	if comma < 0 {
		return nil, "", false
	}
	meta := strings.ToLower(s[len("data:"):comma])
	if !strings.HasPrefix(meta, "image/") || !strings.HasSuffix(meta, ";base64") {
		return nil, "", false
	}
	payload := strings.TrimRight(s[comma+1:], "=")
	// 先按长度拦，再解码：一段几十 MB 的文字没必要解出来才发现太大。
	if base64.RawStdEncoding.DecodedLen(len(payload)) > cacheMaxImageBytes {
		return nil, "", false
	}
	data, err := base64.RawStdEncoding.DecodeString(payload)
	if err != nil || len(data) == 0 {
		return nil, "", false
	}
	ct := sniffImage(data)
	if ct == "" {
		return nil, "", false
	}
	return data, ct, true
}

// inlineDataImage is one picture a body carries inside itself, decoded.
type inlineDataImage struct {
	key         string
	data        []byte
	contentType string
}

// dataImagesIn lists the distinct pictures one body carries inside itself.
//
// 按 key 去重：会话里同一张图被一轮轮引用，每一轮都是同一段原文。上限和外链
// 图片共用那两个数——一封信六十张、合计 15 MB。
func dataImagesIn(html string) []inlineDataImage {
	seen := map[string]bool{}
	var out []inlineDataImage
	total := 0
	mapImageURLs(html, func(raw string) string {
		key := dataImageKey(raw)
		if key == "" || seen[key] || len(out) >= cacheMaxImagesPerMail {
			return raw
		}
		seen[key] = true
		data, ct, ok := decodeDataImage(raw)
		if !ok || total+len(data) > cacheMaxTotalBytes {
			return raw
		}
		total += len(data)
		out = append(out, inlineDataImage{key: key, data: data, contentType: ct})
		return raw
	})
	return out
}

// withInlinePictures is the swap applied before the sanitiser: the message's
// embedded parts plus the pictures its body carried inside itself.
//
// 这些图必须在净化**之前**换。净化器只放行 http/https 的图片地址，data: 进去
// 出来就是一个没有 src 的 <img>，净化之后再换已经无从换起。外链图片正相反，
// 要在净化之后换（见 GetInbound 那段说明）；两种都在 cached 里，这里只挑出
// 自带的那几张。返回新的 map，不改传进来的——会话视图里那几个 map 是按信
// 共用的。
func withInlinePictures(embedded, cached imageSwap) imageSwap {
	var out imageSwap
	for k, v := range cached {
		if !strings.HasPrefix(k, dataImageKeyPrefix) {
			continue
		}
		if out == nil {
			out = make(imageSwap, len(embedded)+1)
			maps.Copy(out, embedded)
		}
		out[k] = v
	}
	if out == nil {
		return embedded
	}
	return out
}

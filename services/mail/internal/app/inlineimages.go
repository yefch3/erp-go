package app

import (
	"context"
	"io"
	"strings"
)

// InlineImage is a picture that travels inside the message rather than being
// fetched from us afterwards.
type InlineImage struct {
	// What the body points at: <img src="cid:THIS">. The image's own token,
	// which is already random and unique, so nothing further is needed to keep
	// two pictures in one message apart.
	ContentID   string
	FileName    string
	ContentType string
	Data        []byte
}

// InlineMailImages turns our own image links into parts carried by the message.
//
// The picture stops being something the recipient must come and fetch and
// becomes something they already have. Three things follow from that, in
// descending order of how much they matter:
//
//   - **It displays.** Remote images are blocked by default in Outlook and
//     Thunderbird, and proxied by Gmail. A signature logo behind "click to
//     show images" is a logo nobody sees. An inline part raises no such
//     question, because looking at it tells the sender nothing.
//   - **It needs no public address.** The http route only works once
//     MAIL_PUBLIC_BASE_URL points at something the outside world can reach.
//     Until then every recipient gets a broken icon — and the sender cannot
//     tell, because for them the address resolves.
//   - **It survives us.** A withdrawn token, an expired bucket lifecycle rule
//     or an afternoon of downtime cannot reach into mail already delivered.
//
// What it costs: the bytes ride along with every copy, and a delivered picture
// cannot be withdrawn. Both are accepted deliberately — a signature logo is a
// few kilobytes, and "unsend the logo" is not a thing anybody wants.
//
// The tracking pixel is untouched by design. It is on a different route
// (/api/public/mail-open/), and turning it into an inline part would defeat
// its entire purpose: it works precisely *because* the recipient has to come
// and fetch it.
//
// Images this cannot resolve — a withdrawn token, a row that vanished, bytes
// that will not read — are left as they are, to be absolutised into an http
// link by the caller. A missing logo must never cost somebody their send.
// 两类图片，同一个待遇。我们自己发布过的（/api/public/mail-images/，签名图和
// 写信时插的图）走 token；回复引用里借来的（对象存储上的收件附件）走 key，
// 见 inlineQuotedStorageImages。后者原来完全没人管，于是一条会过期的地址就
// 那样发给了客户。
func (s *Service) InlineMailImages(
	ctx context.Context, tenantID, ownerID int64, html string,
) (string, []InlineImage) {
	if s.files == nil || html == "" {
		return html, nil
	}
	html, published := s.inlinePublishedImages(ctx, html)
	html, quoted := s.inlineQuotedStorageImages(ctx, tenantID, ownerID, html)
	return html, append(published, quoted...)
}

func (s *Service) inlinePublishedImages(ctx context.Context, html string) (string, []InlineImage) {
	tokens := mailImageTokens(html)
	if len(tokens) == 0 {
		return html, nil
	}

	out := make([]InlineImage, 0, len(tokens))
	embedded := map[string]bool{}
	for _, tok := range tokens {
		row, err := s.q.ResolveImage(ctx, tok)
		if err != nil {
			// Withdrawn or deleted. Leaving the link alone is the honest
			// outcome: the public route will answer 404 and the recipient
			// sees a broken image, which is what a withdrawn picture is.
			s.log.Info("an inline image could not be resolved, leaving it as a link",
				"token", tok)
			continue
		}
		rc, err := s.files.Get(ctx, row.FileKey)
		if err != nil {
			s.log.Warn("an inline image could not be read, leaving it as a link",
				"token", tok, "err", err)
			continue
		}
		data, err := io.ReadAll(io.LimitReader(rc, MaxImageBytes+1))
		rc.Close()
		if err != nil || len(data) == 0 || int64(len(data)) > MaxImageBytes {
			s.log.Warn("an inline image was unreadable or oversized, leaving it as a link",
				"token", tok, "bytes", len(data))
			continue
		}
		out = append(out, InlineImage{
			ContentID:   tok,
			FileName:    "image" + extensionFor(strings.ToLower(row.ContentType)),
			ContentType: row.ContentType,
			Data:        data,
		})
		embedded[tok] = true
	}
	if len(out) == 0 {
		return html, nil
	}

	// Rewrite only the ones actually carried. A cid: pointing at a part that
	// is not in the message is worse than the link it replaced: the link at
	// least has a chance of loading.
	rewritten := mailImagePath.ReplaceAllStringFunc(html, func(m string) string {
		tok := lastPathSegment(m)
		if !embedded[tok] {
			return m
		}
		return "cid:" + tok
	})
	return rewritten, out
}

// mailImageTokens lists the distinct tokens the body references, in the order
// they first appear. Distinct because one logo used twice is one part carried
// once with two references to it.
func mailImageTokens(html string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range mailImagePath.FindAllString(html, -1) {
		tok := lastPathSegment(m)
		if tok == "" || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}

func lastPathSegment(u string) string {
	if i := strings.LastIndex(u, "/"); i >= 0 {
		return u[i+1:]
	}
	return ""
}

package app

import (
	"context"
	"encoding/base64"
	"strings"

	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// trackingPixel is a 1x1 fully transparent GIF, 43 bytes.
//
// GIF rather than PNG because a handful of older clients still refuse to
// render a 1x1 PNG, and the point of this file is to be fetched.
var trackingPixel, _ = base64.StdEncoding.DecodeString(
	"R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7")

// TrackingPixel returns the bytes and content type to serve.
func TrackingPixel() ([]byte, string) { return trackingPixel, "image/gif" }

// InjectOpenPixel appends the tracking image to an HTML body.
//
// HTML only: a plain-text part cannot load an image, and adding a URL to it
// would just show the recipient a stray link.
//
// What this measures is worth stating plainly, because the number it produces
// gets read as "the customer read it" and it is not that:
//
//   - Apple Mail pre-fetches every remote image whether or not the message is
//     ever opened, so it reports opens that did not happen.
//   - Outlook and most corporate clients block remote images by default, so
//     it misses opens that did.
//
// It is a weak signal in both directions. The UI says "可能已打开" for that
// reason, and the only unambiguous evidence a customer read something remains
// the fact that they replied.
func InjectOpenPixel(html, baseURL, messageKey string) string {
	if strings.TrimSpace(html) == "" || baseURL == "" || messageKey == "" {
		return html
	}
	// 同一个常量供 stripOwnPixel 使用：改了这里而没改那边，读信时就会重新
	// 开始误报自己的已读。
	url := strings.TrimSuffix(baseURL, "/") + openPixelPath + messageKey

	// width/height as attributes as well as CSS: Outlook's renderer ignores
	// the style on an img often enough to leave a visible gap otherwise.
	img := `<img src="` + url + `" width="1" height="1" border="0" alt="" ` +
		`style="display:block;width:1px;height:1px;border:0;outline:none;" />`

	// Inside </body> when there is one, so the pixel sits within the document
	// rather than trailing after it — some clients drop content after </body>.
	if i := strings.LastIndex(strings.ToLower(html), "</body>"); i >= 0 {
		return html[:i] + img + html[i:]
	}
	return html + img
}

// RecordOpen notes that a tracking pixel was fetched.
//
// Deliberately tolerant of an unknown key: the caller serves the same image
// either way, so nothing here should distinguish a real send from a probe.
func (s *Service) RecordOpen(ctx context.Context, messageKey, userAgent, ip string) {
	if messageKey == "" {
		return
	}
	row, err := s.q.FindMessageByKeyAnyTenant(ctx, messageKey)
	if err != nil {
		return
	}
	// First open only for the timestamp; every fetch still appends an event,
	// so a message opened repeatedly is distinguishable from one opened once.
	if err := s.q.MarkOpened(ctx, store.MarkOpenedParams{
		TenantID: row.TenantID, ID: row.ID,
	}); err != nil {
		s.log.Warn("could not record an open", "message", row.ID, "err", err)
		return
	}
	detail := userAgent
	if len(detail) > 200 {
		detail = detail[:200]
	}
	_ = s.q.AppendEvent(ctx, store.AppendEventParams{
		TenantID: row.TenantID, MessageID: row.ID, Kind: "OPEN", Detail: detail,
	})
}

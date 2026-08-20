package app

import (
	"context"
	"encoding/base64"
	"net/netip"
	"strings"
	"time"

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
//   - Security gateways load the image while scanning, before the recipient
//     sees anything at all.
//   - Outlook and most corporate clients block remote images by default, so
//     it misses opens that did.
//
// classifyFetch removes what it can recognise of the first two, which narrows
// the error without closing it: a scanner we have not seen before still counts,
// and a blocked image still cannot. It stays a weak signal in both directions,
// the UI says "可能已打开" for that reason, and the only unambiguous evidence a
// customer read something remains the fact that they replied.
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
//
// Every fetch is written to the event log with the address and agent that made
// it, but only one that classifyFetch accepts as a person moves opened_at.
// Recording all of them and counting some of them is the point: a fetch we
// rejected is still the best evidence available if a customer later disputes
// what we told our own user, and the rules will need adjusting against real
// traffic rather than against a guess.
func (s *Service) RecordOpen(ctx context.Context, messageKey, userAgent, ip string) {
	if messageKey == "" {
		return
	}
	row, err := s.q.FindMessageByKeyAnyTenant(ctx, messageKey)
	if err != nil {
		return
	}

	// Zero when the message was never marked sent, which classifyFetch reads
	// as "unknown" and skips the timing rule for.
	var sinceSent time.Duration
	if row.SentAt.Valid {
		sinceSent = time.Since(row.SentAt.Time)
	}
	verdict := classifyFetch(userAgent, row.ToEmail, sinceSent)

	// The agent is what the fetcher says it is; the address is where it
	// actually came from. Only consult the address when the agent looked
	// innocent — the second opinion is two DNS queries, and there is nothing
	// to add once the first has already decided.
	if !verdict.machine {
		verdict = s.origins().classify(ctx, sanitiseIP(ip))
	}

	if !verdict.machine {
		// First open only for the timestamp; every fetch still appends an
		// event, so a message opened repeatedly is distinguishable from one
		// opened once.
		if err := s.q.MarkOpened(ctx, store.MarkOpenedParams{
			TenantID: row.TenantID, ID: row.ID,
		}); err != nil {
			s.log.Warn("could not record an open", "message", row.ID, "err", err)
			return
		}
	}

	_ = s.q.AppendEvent(ctx, store.AppendEventParams{
		TenantID:  row.TenantID,
		MessageID: row.ID,
		Kind:      "OPEN",
		Ip:        sanitiseIP(ip),
		UserAgent: clampAgent(userAgent),
		IsProxy:   verdict.machine,
		Detail:    verdict.reason,
	})
}

// sanitiseIP returns addr if it really is one, and "" otherwise.
//
// The address reaches us from X-Forwarded-For, which any client can set to any
// string it likes. The column is INET, so a value that is not an address makes
// the insert fail — and AppendEvent's error is discarded because a recipient
// must never wait on our bookkeeping, which would turn a junk header into a
// silently dropped event. Dropping just the address keeps the rest of the
// record: the query maps "" to NULL.
func sanitiseIP(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if a, err := netip.ParseAddr(addr); err == nil {
		return a.String()
	}
	// Some proxies forward host:port rather than a bare address.
	if ap, err := netip.ParseAddrPort(addr); err == nil {
		return ap.Addr().String()
	}
	return ""
}

// clampAgent bounds a User-Agent before it is stored.
//
// The header is attacker-controlled and unbounded; nothing legitimate comes
// close to this length, and the column is only ever read by a person trying to
// work out who fetched something.
func clampAgent(ua string) string {
	const maxAgent = 400
	if len(ua) > maxAgent {
		return ua[:maxAgent]
	}
	return ua
}

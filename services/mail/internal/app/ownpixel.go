package app

import (
	"net/url"
	"strings"
)

// blankPixel is a 1×1 transparent GIF as a data: URI — the same bytes the
// tracking route serves, minus the network round trip that is the whole point
// of removing it.
//
// Substituted rather than deleted so the markup keeps its shape: an <img> that
// vanishes can change a layout that was built around it, and a body somebody
// may later export or forward should differ from the original in as few ways
// as possible.
const blankPixel = "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7"

// stripOwnPixel neutralises our own tracking pixels in a body on its way to a
// reader.
//
// The pixel is ours, it is invisible, and fetching it records an open. So when
// one of our own people opens a sent mail — or a reply that quotes it back —
// their browser would report the customer as having read a message the
// customer may never have seen.
//
// The server-side image cache already refuses to fetch our own host
// (cacheableURL), but that guard only stops *the server*. Anything it does not
// cache is left in the markup exactly as it was, which means the address
// survives all the way to the browser and the browser fetches it. Half the
// guard was in place and the wrong half was missing: 对方已读 lit up as soon as
// anybody here looked at their own sent mail.
//
// Only our own host, and only the tracking route. A pixel belonging to
// somebody else is their business and is handled by the image cache; a real
// picture served from our own host — an attachment preview, a signature logo —
// must keep working.
func stripOwnPixel(html, selfHost string) string {
	if html == "" || selfHost == "" || !strings.Contains(html, openPixelPath) {
		return html
	}
	return mapImageURLs(html, func(raw string) string {
		if isOwnPixelURL(raw, selfHost) {
			return blankPixel
		}
		return raw
	})
}

// openPixelPath is the route the pixel lives on. Kept next to the check that
// uses it rather than rebuilt from the tracking URL builder, because a change
// to one without the other should fail visibly in tests, not silently in
// production.
const openPixelPath = "/api/public/mail-open/"

func isOwnPixelURL(raw, selfHost string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if !strings.EqualFold(u.Hostname(), selfHost) {
		return false
	}
	return strings.HasPrefix(u.EscapedPath(), openPixelPath)
}

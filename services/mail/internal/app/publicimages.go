package app

import (
	"regexp"
	"strings"
)

// mailImagePath is our own public image route, wherever it was written and
// whatever host somebody put in front of it.
//
// The host is deliberately loose — anything up to the path, or nothing at all
// for a relative reference — because the whole point is that the host in the
// stored body cannot be trusted to be the one a recipient can reach.
var mailImagePath = regexp.MustCompile(
	`(?i)(?:https?://[^"'\s>]*?)?/api/public/mail-images/([A-Za-z0-9_-]+)`)

// AbsolutiseMailImages rewrites our own image links to the address the outside
// world can actually reach, immediately before the mail leaves.
//
// The composer builds these links from window.location.origin, because that is
// the only address a browser knows. On a developer's machine that is
// http://localhost:5173; behind nginx it is whatever host the employee happened
// to type. Neither is meaningful to a recipient: "localhost" resolves to the
// *reader's* own machine, so a signature logo arrives as a broken icon for
// everybody, every time — and it looks perfect to the sender, because for them
// that address really does work.
//
// Fixed here rather than in the browser for three reasons. The browser cannot
// know MAIL_PUBLIC_BASE_URL. This is the last point before the bytes leave, so
// it covers bodies written by any client, now or later. And it repairs what is
// already stored — signatures and drafts holding a localhost link are corrected
// on the way out instead of needing a migration.
//
// Only our own route is touched. A customer's logo hosted on their own site is
// left exactly as it is; rewriting somebody else's host would break the link
// rather than fix it.
//
// With no public address configured the body is returned untouched: a mail with
// a wrong link is worse than one with the sender's own, which at least works
// for the sender testing locally. The same condition already silences the open
// pixel, and for the same reason.
func AbsolutiseMailImages(html, baseURL string) string {
	base := strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	if base == "" || html == "" {
		return html
	}
	return mailImagePath.ReplaceAllString(html, base+"/api/public/mail-images/$1")
}

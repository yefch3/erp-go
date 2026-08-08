package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// Fetching a received mail's pictures ourselves, once, when it arrives.
//
// See migration 00029 for why. The short version: a picture hosted by the
// sender turns "somebody opened this mail" into an event the sender observes,
// and one-pixel invisible images exist for exactly that. Fetching at delivery
// severs the link — the sender learns the mail was delivered, which they knew.
//
// Two things about this code are load-bearing and easy to lose in a later
// edit, so they are stated here as well as at their definitions:
//
//   - **It fetches URLs chosen by strangers.** Every address in here came out
//     of a mail written by somebody outside the company, and it is fetched by
//     a process sitting inside our network. blockedAddress is what stops that
//     from being a way to read our own internal services, or a cloud
//     provider's metadata endpoint — which hands out credentials to anything
//     that asks. The check runs on the resolved IP immediately before connect,
//     not on the hostname, because a hostname can resolve to whatever the
//     person who owns it wants, including after a first, innocent answer.
//   - **It must never fetch our own tracking pixel.** A customer replying
//     quotes our mail back at us, pixel and all. Fetching it would mark our
//     own message "opened" by the act of receiving the reply. Eight messages
//     already in this mailbox carry one.

const (
	// Per message. Sixty is past any real mail; the largest here references
	// a hundred and fourteen, nearly all of them repeats of the same handful
	// through a quoted chain, and those are deduplicated before fetching.
	cacheMaxImagesPerMail = 60
	cacheMaxImageBytes    = 3 << 20  // one picture
	cacheMaxTotalBytes    = 15 << 20 // one message
	cacheFetchTimeout     = 8 * time.Second
	cacheDialTimeout      = 5 * time.Second
	cacheConcurrency      = 6
	// How many messages one pass takes. Small, because each one may spend
	// seconds on somebody else's slow server and the pass should stay
	// interruptible.
	cacheBatch = 10
	// Between passes once the queue is empty.
	cacheIdleInterval = 30 * time.Second
	// Between passes while there is still a backlog. Short, but not zero:
	// the backfill should not saturate the outbound link on the day it ships.
	cacheBusyInterval = 2 * time.Second
	cacheMaxRedirects = 3
)

var errBlockedAddress = errors.New("address is not publicly routable")

// cacheableTypes is what may be stored and later handed back to a browser.
//
// Decided from the bytes, never from the sender's Content-Type header. The
// picture is served through a presigned URL carrying the type recorded here;
// a sender who could get text/html recorded would have their markup rendered
// from the storage origin, which is a different origin from ours but still
// one the reader trusts enough to have followed a link into.
var cacheableTypes = map[string]bool{
	"image/png":                true,
	"image/jpeg":               true,
	"image/gif":                true,
	"image/webp":               true,
	"image/bmp":                true,
	"image/x-icon":             true,
	"image/vnd.microsoft.icon": true,
}

// ---------------------------------------------------------------- addresses

// routableIP reports whether an address belongs to the public internet.
//
// Everything else is refused: loopback and the private ranges reach our own
// services, which trust the gateway and ask nothing of anyone who can connect
// to them; link-local covers 169.254.169.254, the cloud metadata endpoint that
// answers with instance credentials; and the carrier-grade NAT block holds
// Alibaba Cloud's 100.100.100.200, which does the same thing.
func routableIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		// 100.64.0.0/10, carrier-grade NAT.
		if v4[0] == 100 && v4[1] >= 64 && v4[1] < 128 {
			return false
		}
		// 0.0.0.0/8 and 240.0.0.0/4, neither of which is a real destination.
		if v4[0] == 0 || v4[0] >= 240 {
			return false
		}
	}
	return true
}

// imageClient is an HTTP client that will not talk to our own network.
//
// The check lives in Dialer.Control rather than in a hostname allowlist
// because that is the only place it cannot be walked around: it runs after
// DNS, on the address actually about to be connected to, for the first
// request and for every redirect alike. A hostname check would be beaten by
// a name that resolves publicly once and privately the second time.
func imageClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: cacheDialTimeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if !routableIP(net.ParseIP(host)) {
				return fmt.Errorf("%w: %s", errBlockedAddress, host)
			}
			return nil
		},
	}
	return &http.Client{
		Timeout: cacheFetchTimeout,
		Transport: &http.Transport{
			DialContext:         dialer.DialContext,
			TLSHandshakeTimeout: cacheDialTimeout,
			DisableKeepAlives:   false,
			MaxIdleConns:        cacheConcurrency * 2,
		},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= cacheMaxRedirects {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}

// ------------------------------------------------------------------- URLs

// One function finds the picture addresses in a body and one function
// replaces them, and they are the same function. Extraction and substitution
// disagreeing would mean fetching a picture and then failing to use it, or
// worse, rewriting something that was never a picture — a URL inside an href
// turned into a signed download link.
var (
	imgSrcAttr = regexp.MustCompile(`(?i)(<img\b[^>]*?\bsrc\s*=\s*)("[^"]*"|'[^']*')`)
	srcsetAttr = regexp.MustCompile(`(?i)(\bsrcset\s*=\s*)("[^"]*"|'[^']*')`)
	cssURL     = regexp.MustCompile(`(?i)\burl\(\s*("[^"]*"|'[^']*'|[^)\s]+)\s*\)`)
)

// mapImageURLs applies f to every address the body uses as a picture, and
// returns the body with f's answers in place. An f that returns its input
// unchanged makes this a pure scan.
func mapImageURLs(html string, f func(string) string) string {
	out := imgSrcAttr.ReplaceAllStringFunc(html, func(m string) string {
		g := imgSrcAttr.FindStringSubmatch(m)
		q, raw := unquote(g[2])
		return g[1] + q + f(raw) + q
	})
	out = srcsetAttr.ReplaceAllStringFunc(out, func(m string) string {
		g := srcsetAttr.FindStringSubmatch(m)
		q, raw := unquote(g[2])
		return g[1] + q + mapSrcset(raw, f) + q
	})
	return cssURL.ReplaceAllStringFunc(out, func(m string) string {
		g := cssURL.FindStringSubmatch(m)
		q, raw := unquote(g[1])
		return "url(" + q + f(raw) + q + ")"
	})
}

// mapSrcset walks "a.png 1x, b.png 2x": each entry is an address followed by
// an optional descriptor, and only the address is ours to change.
func mapSrcset(v string, f func(string) string) string {
	parts := strings.Split(v, ",")
	for i, p := range parts {
		lead := p[:len(p)-len(strings.TrimLeft(p, " \t\r\n"))]
		fields := strings.Fields(p)
		if len(fields) == 0 {
			continue
		}
		rest := ""
		if len(fields) > 1 {
			rest = " " + strings.Join(fields[1:], " ")
		}
		parts[i] = lead + f(fields[0]) + rest
	}
	return strings.Join(parts, ",")
}

func unquote(s string) (quote, inner string) {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return string(s[0]), s[1 : len(s)-1]
	}
	return "", s
}

// cacheableURL decides whether an address is worth fetching at all.
//
// selfHost is our own public host: a customer's reply quotes our mail back,
// tracking pixel included, and fetching it would record the customer as
// having opened a message they may never have opened. That check has to be
// here rather than in routableIP, because on a real deployment our own public
// address *is* publicly routable.
func cacheableURL(raw, selfHost string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "data:") || strings.HasPrefix(raw, "cid:") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", false
	}
	if selfHost != "" && host == selfHost {
		return "", false
	}
	return raw, true
}

// remoteImagesIn lists the distinct addresses one body would fetch.
//
// Distinct matters more than it looks: a quoted reply chain repeats the
// sender's signature logo once per turn, so a sixteen-message thread names
// the same picture sixteen times. Deduplicating here is the difference
// between six fetches and ninety-six.
func remoteImagesIn(html, selfHost string) []string {
	seen := map[string]bool{}
	var out []string
	mapImageURLs(html, func(raw string) string {
		if u, ok := cacheableURL(raw, selfHost); ok && !seen[u] {
			seen[u] = true
			if len(out) < cacheMaxImagesPerMail {
				out = append(out, u)
			}
		}
		return raw
	})
	return out
}

func urlHash(u string) []byte {
	sum := sha256.Sum256([]byte(u))
	return sum[:]
}

// ------------------------------------------------------------- the fetching

type fetchedImage struct {
	url         string
	data        []byte
	contentType string
}

// fetchImages pulls what it can inside the budget and skips the rest.
//
// Partial success is the expected outcome, not a failure: senders take images
// down, hotlink-protect them, or answer slowly. Every picture that does not
// arrive simply keeps pointing at the sender, which is exactly the behaviour
// this whole change replaces — worse for that one picture, no worse than
// today.
func fetchImages(ctx context.Context, client *http.Client, urls []string) []fetchedImage {
	var (
		mu    sync.Mutex
		total int64
		out   []fetchedImage
		wg    sync.WaitGroup
	)
	sem := make(chan struct{}, cacheConcurrency)
	for _, u := range urls {
		select {
		case <-ctx.Done():
			wg.Wait()
			return out
		default:
		}
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			mu.Lock()
			over := total >= cacheMaxTotalBytes
			mu.Unlock()
			if over {
				return
			}
			img, err := fetchOneImage(ctx, client, u)
			if err != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if total+int64(len(img.data)) > cacheMaxTotalBytes {
				return
			}
			total += int64(len(img.data))
			out = append(out, img)
		}(u)
	}
	wg.Wait()
	return out
}

func fetchOneImage(ctx context.Context, client *http.Client, u string) (fetchedImage, error) {
	// The address as HTML spells it is not the address HTTP wants. An
	// attribute value is entity-encoded, so every "&" between query
	// parameters arrives here as "&amp;" — and fetching that literally asks
	// the sender's server for a parameter called "amp;d" instead of "d".
	//
	// It matters most where it hurts most: a plain picture has no query string
	// and was unaffected, while a tracking pixel is nothing but query string.
	// Measured before the fix, 360 stored rows still carried "&amp;" in the
	// address, and those are only the ones whose server was forgiving enough
	// to answer anyway; the rest failed silently and stayed uncached.
	//
	// Only the request is decoded. What gets stored stays exactly as the body
	// spells it, because that is the string the read path matches against.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, html.UnescapeString(u), nil)
	if err != nil {
		return fetchedImage{}, err
	}
	// A plain, honest identification. Not a browser's string: pretending to
	// be one to get past hotlink protection would be helping ourselves to
	// something the host is deliberately withholding.
	req.Header.Set("User-Agent", "ERPMailImageFetcher/1.0")
	req.Header.Set("Accept", "image/*")
	resp, err := client.Do(req)
	if err != nil {
		return fetchedImage{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fetchedImage{}, fmt.Errorf("status %d", resp.StatusCode)
	}
	// One byte past the cap, so a file exactly at the limit still fits and
	// anything larger is detectably truncated rather than silently accepted.
	data, err := io.ReadAll(io.LimitReader(resp.Body, cacheMaxImageBytes+1))
	if err != nil {
		return fetchedImage{}, err
	}
	if len(data) == 0 || int64(len(data)) > cacheMaxImageBytes {
		return fetchedImage{}, errors.New("empty or oversized")
	}
	ct := sniffImage(data)
	if ct == "" {
		return fetchedImage{}, errors.New("not an image")
	}
	return fetchedImage{url: u, data: data, contentType: ct}, nil
}

// sniffImage reads the type out of the bytes. http.DetectContentType returns
// a bare type for images, so the map lookup is exact.
func sniffImage(data []byte) string {
	ct := http.DetectContentType(data)
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	if cacheableTypes[strings.ToLower(ct)] {
		return ct
	}
	return ""
}

// ------------------------------------------------------------------ the pass

// RunImageCache keeps fetching the pictures of messages that have none yet.
//
// A pass of its own rather than a step inside ingest. Ingest runs under the
// mailbox sync's overall timeout, and one message with twenty pictures on a
// slow host would spend longer than that budget by itself — mail would stop
// arriving because a marketing server was having a bad afternoon. Separating
// them also means this doubles as the backfill: every message already in the
// mailbox has a null stamp and joins the queue on first start.
func (s *Service) RunImageCache(ctx context.Context, cfg SyncConfig) {
	cfg = cfg.withDefaults()
	if s.files == nil {
		s.log.Info("image cache disabled: no object storage configured")
		return
	}
	client := imageClient()
	selfHost := publicHostOf(cfg.PublicBaseURL)
	s.log.Info("mail image cache started", "self_host", selfHost)

	for {
		n, err := s.cacheImagesOnce(ctx, cfg.TenantID, client, selfHost)
		if err != nil && ctx.Err() == nil {
			s.log.Warn("image cache pass failed", "err", err)
		}
		wait := cacheIdleInterval
		if n > 0 {
			wait = cacheBusyInterval
		}
		select {
		case <-ctx.Done():
			s.log.Info("mail image cache stopped")
			return
		case <-time.After(wait):
		}
	}
}

// cacheImagesOnce handles one batch and reports how many messages it stamped.
func (s *Service) cacheImagesOnce(ctx context.Context, tenantID int64, client *http.Client, selfHost string) (int, error) {
	rows, err := s.q.ListInboundNeedingImages(ctx, store.ListInboundNeedingImagesParams{
		TenantID: tenantID, RowLimit: cacheBatch,
	})
	if err != nil {
		return 0, err
	}
	done := 0
	for _, r := range rows {
		if ctx.Err() != nil {
			return done, ctx.Err()
		}
		s.cacheImagesFor(ctx, tenantID, r.ID, r.BodyHtml, client, selfHost)
		// Stamped whatever happened. A message that could not be fetched has
		// still had its turn; leaving it unstamped would put a dead host at
		// the head of the queue for ever.
		if err := s.q.MarkImagesCached(ctx, store.MarkImagesCachedParams{
			TenantID: tenantID, ID: r.ID,
		}); err != nil {
			s.log.Warn("could not stamp a message as cached", "id", r.ID, "err", err)
			// Not counted as progress: if stamping is what is broken, the
			// caller must not keep spinning on the same batch.
			continue
		}
		done++
	}
	return done, nil
}

func (s *Service) cacheImagesFor(ctx context.Context, tenantID, inboundID int64, html string, client *http.Client, selfHost string) {
	// The addresses are read from the repaired body, because that is the
	// form the reader will match against later — repairURLWhitespace percent-
	// encodes spaces on the way out, and a URL stored in its unrepaired shape
	// would never be found again.
	urls := remoteImagesIn(repairURLWhitespace(html), selfHost)
	if len(urls) == 0 {
		return
	}
	for _, img := range fetchImages(ctx, client, urls) {
		sum := sha256.Sum256(img.data)
		key := fmt.Sprintf("mail/inbound-img/%d/%d/%s%s",
			tenantID, inboundID, hex.EncodeToString(sum[:8]), extensionFor(img.contentType))
		if err := s.putRaw(ctx, key, img.data); err != nil {
			s.log.Warn("could not store a cached picture", "id", inboundID, "err", err)
			continue
		}
		if err := s.q.InsertInboundImage(ctx, store.InsertInboundImageParams{
			TenantID: tenantID, InboundID: inboundID,
			SourceUrl: img.url, UrlHash: urlHash(img.url),
			ObjectKey: key, ContentType: img.contentType,
			ByteSize: int64(len(img.data)),
		}); err != nil {
			s.log.Warn("could not record a cached picture", "id", inboundID, "err", err)
		}
	}
}

func extensionFor(contentType string) string {
	switch contentType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	default:
		return ".img"
	}
}

// publicHostOf is our own host as a mail would spell it, or empty when no
// public address is configured — in which case there is no pixel of ours in
// the wild to trip over.
func publicHostOf(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// ------------------------------------------------------------- the read path

// imageSwap resolves a message's cached pictures into signed, time-limited
// addresses on the storage origin, the same way an attachment download works:
// the bytes never pass through the gateway.
type imageSwap map[string]string

// localiseImages puts our own copies into a body on its way to the reader.
//
// Anything not cached is left exactly as it was. That is the deliberate
// degradation: a picture we failed to fetch still loads from the sender, so
// the mail looks right and the reader is no worse off than before this
// existed — the privacy gain applies to what we did manage to fetch, which in
// practice is nearly everything, because a tracking pixel is a 43-byte file
// its owner very much wants to serve.
func (s *Service) localiseImages(ctx context.Context, html string, swap imageSwap) string {
	if len(swap) == 0 || !strings.Contains(html, "http") {
		return html
	}
	_ = ctx
	return mapImageURLs(html, func(raw string) string {
		if signed, ok := swap[strings.TrimSpace(raw)]; ok {
			return signed
		}
		return raw
	})
}

// swapForMessage signs every cached picture of one message.
func (s *Service) swapForMessage(ctx context.Context, tenantID, inboundID int64) imageSwap {
	if s.files == nil {
		return nil
	}
	rows, err := s.q.ListInboundImages(ctx, store.ListInboundImagesParams{
		TenantID: tenantID, InboundID: inboundID,
	})
	if err != nil || len(rows) == 0 {
		return nil
	}
	swap := make(imageSwap, len(rows))
	for _, r := range rows {
		if u := s.signImage(ctx, r.ObjectKey, r.ContentType); u != "" {
			swap[r.SourceUrl] = u
		}
	}
	return swap
}

// swapForThread does the same for a whole conversation, keyed by message.
func (s *Service) swapForThread(ctx context.Context, tenantID, ownerID int64, threadKey string) map[int64]imageSwap {
	if s.files == nil || threadKey == "" {
		return nil
	}
	rows, err := s.q.ListThreadImages(ctx, store.ListThreadImagesParams{
		TenantID: tenantID, OwnerID: ownerID, ThreadKey: threadKey,
	})
	if err != nil || len(rows) == 0 {
		return nil
	}
	// One signature per distinct object, not per reference: a signature is
	// cheap but a quoted chain names the same logo in every turn.
	signed := map[string]string{}
	out := map[int64]imageSwap{}
	for _, r := range rows {
		u, ok := signed[r.ObjectKey]
		if !ok {
			u = s.signImage(ctx, r.ObjectKey, r.ContentType)
			signed[r.ObjectKey] = u
		}
		if u == "" {
			continue
		}
		if out[r.InboundID] == nil {
			out[r.InboundID] = imageSwap{}
		}
		out[r.InboundID][r.SourceUrl] = u
	}
	return out
}

// signImage asks storage for a URL the browser may fetch directly.
//
// Inline rather than attachment, and with the type we sniffed at fetch time
// rather than whatever the object happens to carry — see PresignGetInline.
func (s *Service) signImage(ctx context.Context, key, contentType string) string {
	if key == "" || !cacheableTypes[strings.ToLower(contentType)] {
		return ""
	}
	u, err := s.files.PresignGetInline(ctx, key, contentType)
	if err != nil {
		s.log.Warn("could not sign a cached picture", "key", key, "err", err)
		return ""
	}
	return u
}

// imageKeysFor lists the stored objects behind one message, for deletion.
func (s *Service) imageKeysFor(ctx context.Context, tenantID, inboundID int64) []string {
	keys, err := s.q.ListImageKeysForPurge(ctx, store.ListImageKeysForPurgeParams{
		TenantID: tenantID, InboundID: inboundID,
	})
	if err != nil {
		s.log.Warn("could not list cached pictures for deletion", "id", inboundID, "err", err)
		return nil
	}
	return keys
}

package app

import (
	"strings"
	"time"
)

// Telling a person apart from a machine at the far end of a tracking pixel.
//
// The pixel measures one thing: somebody fetched an image. Turning that into
// "the customer read it" requires deciding who the somebody was, and a great
// deal of the traffic is not a customer:
//
//   - Security gateways. Proofpoint, Mimecast and Barracuda scan inbound mail
//     by loading its images in a sandbox before the recipient ever sees it.
//     University and corporate domains are almost all behind one — columbia.edu
//     resolves to pphosted.com, which is Proofpoint.
//   - Provider image proxies. Gmail fetches through ggpht.com and says so in
//     the User-Agent.
//   - Apple Mail Privacy Protection, which pre-fetches every remote image on
//     delivery regardless of whether the message is opened.
//
// Counting those as opens is not a small error. It reports a read for a
// message nobody looked at, which is worse than reporting nothing: it invites
// somebody to follow up on a customer who never heard from them.
//
// So every fetch is still recorded — the event log keeps all of it, with the
// address and agent that made the request — but only a fetch that survives
// these checks moves opened_at. What we cannot identify still counts, which
// means this errs toward over-reporting; it narrows the error rather than
// eliminating it, and the UI keeps saying 可能已打开 for that reason.

// scanWindow is how soon after a send a fetch is assumed to be automated.
//
// A security gateway loads the images as part of accepting the message, so its
// fetch lands within seconds. A person has to be told the mail exists, open
// their client and look at it. One minute is long enough to cover a slow scan
// and short enough that a genuine reader is unlikely to be inside it — and if
// somebody really was watching their inbox that closely, they will almost
// certainly open it again later, and that fetch will count.
const scanWindow = time.Minute

// fetchVerdict is what we concluded about one pixel fetch.
//
// reason is stored alongside the event rather than discarded, because the
// judgement will be wrong sometimes and the only way to find out which rule
// was responsible is to have written it down.
type fetchVerdict struct {
	machine bool
	reason  string
}

// machineAgents are User-Agent fragments that identify software directly.
//
// Matched case-insensitively as substrings. Deliberately not an exhaustive
// bot list: this is aimed at what actually shows up in front of a tracking
// pixel, and a list that tries to name every crawler ends up matching a real
// browser by accident.
var machineAgents = []string{
	// Provider image proxies, which announce themselves.
	"yahoomailproxy",
	// Security gateways and scanners.
	"proofpoint",
	"mimecast",
	"barracuda",
	"symantec",
	"forcepoint",
	"trendmicro",
	// Headless browsers — what a sandbox drives.
	"headlesschrome",
	"phantomjs",
	"electron",
	// Plain HTTP clients. No mail client is any of these.
	"curl",
	"wget",
	"python-requests",
	"go-http-client",
	"java/",
	"okhttp",
	"libwww-perl",
	// Generic crawler markers, kept last and kept narrow.
	"bot/",
	"spider",
	"crawler",
	"preview",
}

// googleProxyMarker is how Gmail's image proxy identifies itself.
const googleProxyMarker = "googleimageproxy"

// googleMailboxDomains are the domains where a Gmail proxy fetch plausibly
// means the recipient looked at the message.
//
// Google Workspace domains are missed by this list, since recognising them
// needs an MX lookup and this runs while a recipient's mail client is waiting
// for an image. Missing one costs an under-report on a signal already
// presented as a maybe, which is the safe direction to be wrong in.
var googleMailboxDomains = []string{"gmail.com", "googlemail.com"}

// classifyFetch decides whether a pixel fetch is evidence that a person read
// the message.
//
// sinceSent is the gap between the send and this fetch; a non-positive value
// means we do not know when the message went out (it was never marked sent)
// and the timing rule is skipped rather than guessed at.
func classifyFetch(userAgent, toEmail string, sinceSent time.Duration) fetchVerdict {
	ua := strings.ToLower(strings.TrimSpace(userAgent))

	// No agent at all. Every mail client and browser sends one; something that
	// does not is a script.
	if ua == "" {
		return fetchVerdict{machine: true, reason: "no user-agent"}
	}

	// Gmail's proxy is the ambiguous case and gets its own rule.
	//
	// It fetches when a Gmail user displays a message — which is a real open
	// if that user is the recipient. But our own senders are on Gmail too, and
	// their copy in 已发送 carries the same pixel, so opening one's own sent
	// mail in Gmail reports the customer as having read it. When the recipient
	// is not on Gmail, that is the only thing this fetch can be.
	if strings.Contains(ua, googleProxyMarker) {
		if !googleMailbox(toEmail) {
			return fetchVerdict{machine: true, reason: "gmail proxy, recipient not on gmail"}
		}
		return fetchVerdict{}
	}

	for _, frag := range machineAgents {
		if strings.Contains(ua, frag) {
			return fetchVerdict{machine: true, reason: "agent matched " + frag}
		}
	}

	if sinceSent > 0 && sinceSent < scanWindow {
		return fetchVerdict{machine: true, reason: "fetched within " + scanWindow.String() + " of sending"}
	}

	return fetchVerdict{}
}

// googleMailbox reports whether an address is on a Google consumer mailbox.
func googleMailbox(addr string) bool {
	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return false
	}
	domain := strings.ToLower(strings.TrimSpace(addr[at+1:]))
	for _, d := range googleMailboxDomains {
		if domain == d {
			return true
		}
	}
	return false
}

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

// displayProxies are provider image proxies that fetch because a person
// displayed the message.
//
// These must NOT be filtered, and the distinction is the one this file got
// wrong the first time. Sort proxies by what triggers them, not by the fact
// that they are proxies:
//
//   - Gmail and Yahoo fetch through their proxy when a mailbox displays the
//     message. A person did something. That is exactly the event we are trying
//     to observe, arriving via an intermediary.
//   - Apple's Mail Privacy Protection relay fetches on delivery whether or not
//     anybody opens anything. No person is involved, so it is filtered — by
//     address, in origin.go, since its agent is indistinguishable from a real
//     Apple Mail.
//
// Filtering Gmail's proxy is not a small over-correction. Google Workspace
// hosts an enormous share of business mail, including domains that look like
// anything but Google from the outside — columbia.edu publishes Proofpoint MX
// records and delivers into Google Workspace behind them — so suppressing it
// does not shade the number down, it reports "未检测到打开" forever for a large
// class of perfectly ordinary recipients. That is not a conservative error. It
// is a broken feature.
//
// This does let one false positive through: our own sender viewing their copy
// in 已发送 inside Gmail, which fetches the same pixel. That is a narrower
// problem than the one above and is not worth paying for with the primary
// signal.
var displayProxies = []string{
	"googleimageproxy",
	"ggpht.com",
	"yahoomailproxy",
}

// classifyFetch decides whether a pixel fetch is evidence that a person read
// the message.
//
// sinceSent is the gap between the send and this fetch; a non-positive value
// means we do not know when the message went out (it was never marked sent)
// and the timing rule is skipped rather than guessed at.
func classifyFetch(userAgent string, sinceSent time.Duration) fetchVerdict {
	ua := strings.ToLower(strings.TrimSpace(userAgent))

	// No agent at all. Every mail client and browser sends one; something that
	// does not is a script.
	if ua == "" {
		return fetchVerdict{machine: true, reason: "no user-agent"}
	}

	// A display proxy skips the agent list — it is an intermediary for a
	// person, not a machine acting on its own — but still faces the timing
	// rule below, which catches the minority of provider fetches that happen
	// on delivery rather than on display.
	if !containsAny(ua, displayProxies) {
		for _, frag := range machineAgents {
			if strings.Contains(ua, frag) {
				return fetchVerdict{machine: true, reason: "agent matched " + frag}
			}
		}
	}

	if sinceSent > 0 && sinceSent < scanWindow {
		return fetchVerdict{machine: true, reason: "fetched within " + scanWindow.String() + " of sending"}
	}

	return fetchVerdict{}
}

func containsAny(s string, fragments []string) bool {
	for _, f := range fragments {
		if strings.Contains(s, f) {
			return true
		}
	}
	return false
}

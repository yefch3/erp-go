package app

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// Identifying an automated fetcher by where it came from rather than what it
// says it is.
//
// classifyFetch judges the User-Agent, which the fetcher chooses and can set
// to anything. The address it connects from is far harder to fake, and it
// catches the case the User-Agent cannot: Apple's Mail Privacy Protection
// relay sends an ordinary Apple Mail agent and pre-fetches every image on
// delivery, so nothing in the header distinguishes it from a person.
//
// There are two ways to use an address and they fail differently.
//
// A list of published ranges is exact while it is current and silently wrong
// once it is not. Google publishes its ranges as CIDR and warns in the same
// breath that some Google hosts fall outside them and that other services
// share them, so a list is neither complete nor exclusive — and it is a file
// somebody has to remember to refresh.
//
// Reverse DNS needs no list. Ask what name the address claims, then resolve
// that name and check it comes back to the same address; a forger controls
// neither the reverse zone nor the forward one, so a name that survives the
// round trip is the operator's own word for what the machine is. Verified
// against the address that actually scanned our mail:
//
//	148.163.139.74 → mx0b-00364e01.pphosted.com → 148.163.139.74   ✓ Proofpoint
//	66.249.84.1    → google-proxy-66-249-84-1.google.com           ✓ Gmail proxy
//
// It does not cover everything. Apple's relays publish no reverse record at
// all, so 17.0.0.0/8 has to be a literal range. Hence both mechanisms: names
// first because they need no maintenance, a short list of ranges for the
// operators who publish nothing.

// scannerNets are ranges belonging to an automated fetcher that has no usable
// reverse DNS, so nothing but the range itself identifies it.
//
// Kept deliberately short. Every entry here is a maintenance burden and a
// standing risk of blocking a real reader, so an operator goes in this list
// only after reverse DNS has been shown not to work for it.
var scannerNets = []netip.Prefix{
	// Apple. Mail Privacy Protection relays fetch every remote image on
	// delivery whether or not the message is ever opened, and they present an
	// ordinary Apple Mail agent while doing it. Apple owns the whole /8 and
	// publishes no PTR records for the relays.
	netip.MustParsePrefix("17.0.0.0/8"),
}

// machineHosts are reverse-DNS suffixes that identify an automated fetcher.
//
// Matched against a forward-confirmed name only, so an entry here cannot be
// claimed by someone who does not control the operator's DNS.
var machineHosts = []string{
	"pphosted.com",           // Proofpoint
	"ppops.net",              // Proofpoint
	"mimecast.com",           // Mimecast
	"barracuda.com",          // Barracuda
	"barracudanetworks.com",  // Barracuda
	"messagelabs.com",        // Broadcom/Symantec Email Security
	"iphmx.com",              // Cisco Secure Email
	"protection.outlook.com", // Microsoft Defender for Office 365
	"trendmicro.com",         // Trend Micro
	"fireeye.com",            // Trellix
	"googlebot.com",          // Google crawlers
	// Google's own hosts. A person reading our mail connects from their own
	// network; an image fetch arriving from inside Google is a proxy or a
	// crawler. The Gmail image proxy is judged earlier and more precisely by
	// classifyFetch, which knows who the message was addressed to, so this
	// only catches what that rule did not.
	"google.com",
}

// originTTL is how long one verdict about an address is reused.
//
// Two lookups per address is already the cost of forward confirmation, and a
// scanner fetches the same pixel repeatedly, so without a cache a burst of
// scanning turns into a burst of DNS. An hour is far shorter than the rate at
// which mail infrastructure changes hands.
const originTTL = time.Hour

// originLookupTimeout bounds the pair of DNS queries.
//
// The recipient is not waiting on this — the pixel has already been served and
// this runs on a detached goroutine — but a resolver that never answers must
// not pin one down indefinitely.
const originLookupTimeout = 3 * time.Second

// resolver is the slice of *net.Resolver this needs, named so tests can
// supply their own without a network.
type resolver interface {
	LookupAddr(ctx context.Context, addr string) ([]string, error)
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// originClassifier decides whether an address belongs to an automated fetcher,
// caching what it learns.
type originClassifier struct {
	res resolver

	mu   sync.Mutex
	seen map[string]cachedOrigin
	// now is time.Now except in tests.
	now func() time.Time
}

type cachedOrigin struct {
	verdict fetchVerdict
	at      time.Time
}

func newOriginClassifier(res resolver) *originClassifier {
	if res == nil {
		res = net.DefaultResolver
	}
	return &originClassifier{res: res, seen: map[string]cachedOrigin{}, now: time.Now}
}

// classify reports whether a fetch from addr came from a machine.
//
// An address it cannot make sense of is not a machine. Everything here is a
// reason to *discount* a fetch, and the honest default when we know nothing is
// to leave the fetch counting — over-reporting is the error this whole
// mechanism already admits to, and inventing certainty from a failed DNS
// lookup would be a worse one.
func (o *originClassifier) classify(ctx context.Context, addr string) fetchVerdict {
	ip, err := netip.ParseAddr(addr)
	if err != nil {
		return fetchVerdict{}
	}
	ip = ip.Unmap()

	// Our own network and the machine itself. These show up in development and
	// behind a misconfigured proxy, and neither is evidence of anything.
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return fetchVerdict{}
	}

	for _, n := range scannerNets {
		if n.Contains(ip) {
			return fetchVerdict{machine: true, reason: "address in " + n.String()}
		}
	}

	key := ip.String()
	if v, ok := o.cached(key); ok {
		return v
	}

	v := o.byName(ctx, key)
	o.remember(key, v)
	return v
}

func (o *originClassifier) cached(key string) (fetchVerdict, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	c, ok := o.seen[key]
	if !ok || o.now().Sub(c.at) > originTTL {
		return fetchVerdict{}, false
	}
	return c.verdict, true
}

func (o *originClassifier) remember(key string, v fetchVerdict) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seen[key] = cachedOrigin{verdict: v, at: o.now()}
}

// byName runs the forward-confirmed reverse lookup.
func (o *originClassifier) byName(ctx context.Context, addr string) fetchVerdict {
	ctx, cancel := context.WithTimeout(ctx, originLookupTimeout)
	defer cancel()

	names, err := o.res.LookupAddr(ctx, addr)
	if err != nil || len(names) == 0 {
		return fetchVerdict{}
	}
	for _, name := range names {
		host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
		suffix, ok := matchHost(host)
		if !ok {
			continue
		}
		if !o.confirms(ctx, host, addr) {
			// A reverse record claiming a name the forward zone will not back
			// up. Not evidence, and worth nothing further.
			continue
		}
		return fetchVerdict{machine: true, reason: "reverse dns " + host + " (" + suffix + ")"}
	}
	return fetchVerdict{}
}

// confirms resolves host and reports whether addr is among its addresses.
//
// This is the half that makes reverse DNS trustworthy. Anyone can point a PTR
// record at any name they like; only the owner of that name can make it
// resolve back.
func (o *originClassifier) confirms(ctx context.Context, host, addr string) bool {
	back, err := o.res.LookupHost(ctx, host)
	if err != nil {
		return false
	}
	for _, b := range back {
		if ip, err := netip.ParseAddr(strings.TrimSpace(b)); err == nil {
			if ip.Unmap().String() == addr {
				return true
			}
		}
	}
	return false
}

// matchHost reports which suffix in machineHosts host falls under.
//
// A suffix matches the whole host or a label boundary before it, so
// evil-google.com and google.com.attacker.net do not match google.com.
func matchHost(host string) (string, bool) {
	for _, suffix := range machineHosts {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return suffix, true
		}
	}
	return "", false
}

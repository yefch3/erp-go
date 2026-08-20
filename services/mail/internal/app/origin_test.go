package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeResolver answers from a table so these tests never touch the network.
type fakeResolver struct {
	ptr   map[string][]string // address → names
	fwd   map[string][]string // name → addresses
	txt   map[string][]string // cymru query → answer
	calls int
	fail  bool
}

func (f *fakeResolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	if f.fail {
		return nil, errors.New("dns is down")
	}
	t, ok := f.txt[name]
	if !ok {
		return nil, errors.New("no TXT")
	}
	return t, nil
}

func (f *fakeResolver) LookupAddr(_ context.Context, addr string) ([]string, error) {
	f.calls++
	if f.fail {
		return nil, errors.New("dns is down")
	}
	n, ok := f.ptr[addr]
	if !ok {
		return nil, errors.New("no PTR")
	}
	return n, nil
}

func (f *fakeResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	if f.fail {
		return nil, errors.New("dns is down")
	}
	a, ok := f.fwd[host]
	if !ok {
		return nil, errors.New("no A")
	}
	return a, nil
}

// The two addresses below are real. They were resolved from the mail
// infrastructure that actually fetched our pixels, and the round trip was
// verified by hand before being written down here.
func realWorld() *fakeResolver {
	return &fakeResolver{
		ptr: map[string][]string{
			"148.163.139.74": {"mx0b-00364e01.pphosted.com."},
			"66.249.84.1":    {"google-proxy-66-249-84-1.google.com."},
			"203.0.113.9":    {"host9.customer-isp.example."},
			// A reverse record claiming somebody else's name. The forward zone
			// will not agree, which is the entire point of confirming.
			"198.51.100.5": {"mx0a-00364e01.pphosted.com."},
		},
		fwd: map[string][]string{
			"mx0b-00364e01.pphosted.com":          {"148.163.139.74"},
			"google-proxy-66-249-84-1.google.com": {"66.249.84.1"},
			"host9.customer-isp.example":          {"203.0.113.9"},
			// Genuinely Proofpoint's, and not the address that claimed it.
			"mx0a-00364e01.pphosted.com": {"148.163.135.74"},
		},
		// Answers as Team Cymru actually returned them for the addresses that
		// fetched our pixels.
		txt: map[string][]string{
			"73.15.59.108.origin.asn.cymru.com":  {"30633 | 108.59.0.0/20 | US | arin | 2010-11-18"},
			"126.3.202.38.origin.asn.cymru.com":  {"9009 | 38.202.0.0/22 | US | arin | 1991-04-16"},
			"33.237.63.100.origin.asn.cymru.com": {"14618 | 100.48.0.0/12 | US | arin | 2024-12-12"},
			"224.84.249.66.origin.asn.cymru.com": {"15169 | 66.249.64.0/19 | US | arin | 2004-03-05"},
			"9.113.0.203.origin.asn.cymru.com":   {"64500 | 203.0.113.0/24 | US | arin | 2010-01-01"},
		},
	}
}

func TestOriginClassify(t *testing.T) {
	cases := []struct {
		name    string
		addr    string
		machine bool
	}{
		{"proofpoint gateway, forward confirmed", "148.163.139.74", true},
		// Gmail's proxy fetches because somebody displayed the message.
		// Filtering it by address would undo the displayProxies decision one
		// layer down, so google.com is deliberately not in machineHosts.
		{"gmail image proxy is a person, not a machine", "66.249.84.1", false},
		{"apple relay by range, no PTR needed", "17.58.63.10", true},
		{"a reader on their own isp", "203.0.113.9", false},
		// The three scanners. Byte-identical Chrome/149 agents arriving from
		// three unrelated hosting companies; timing could not separate these
		// from the genuine open at 66 seconds.
		{"leaseweb", "108.59.15.73", true},
		{"m247", "38.202.3.126", true},
		{"aws, no matching reverse name", "100.63.237.33", true},
		// Google runs a cloud too, and must never be filtered as one: it also
		// runs the proxy that fetches for a reader.
		{"google's network is not hosting", "66.249.84.224", false},
		{"an address with no reverse record at all", "192.0.2.44", false},
		{"forged PTR claiming proofpoint is rejected", "198.51.100.5", false},
		{"loopback is not evidence of anything", "127.0.0.1", false},
		{"private address is not evidence of anything", "10.1.2.3", false},
		{"link-local", "169.254.7.7", false},
		{"not an address at all", "definitely-not-an-ip", false},
		{"empty", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := newOriginClassifier(realWorld())
			got := o.classify(context.Background(), tc.addr)
			if got.machine != tc.machine {
				t.Fatalf("machine = %v, want %v (reason %q)", got.machine, tc.machine, got.reason)
			}
			if got.machine && got.reason == "" {
				t.Fatal("a rejected address must say which rule rejected it")
			}
		})
	}
}

// A resolver that is down must not silently turn every reader into a machine.
// Over-reporting is the error this feature already admits to; inventing a
// verdict from a failed lookup would be a worse one.
func TestOriginFailedLookupCountsAsHuman(t *testing.T) {
	o := newOriginClassifier(&fakeResolver{fail: true})
	if v := o.classify(context.Background(), "148.163.139.74"); v.machine {
		t.Fatalf("a DNS failure was treated as a machine: %q", v.reason)
	}
}

// A scanner fetches the same pixel repeatedly. Without the cache that is two
// DNS queries every time.
func TestOriginCachesByAddress(t *testing.T) {
	f := realWorld()
	o := newOriginClassifier(f)

	for i := 0; i < 5; i++ {
		if v := o.classify(context.Background(), "148.163.139.74"); !v.machine {
			t.Fatal("verdict changed between calls")
		}
	}
	if f.calls != 1 {
		t.Fatalf("reverse lookups = %d, want 1", f.calls)
	}
}

func TestOriginCacheExpires(t *testing.T) {
	f := realWorld()
	o := newOriginClassifier(f)

	base := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	o.now = func() time.Time { return base }
	o.classify(context.Background(), "148.163.139.74")

	o.now = func() time.Time { return base.Add(originTTL + time.Minute) }
	o.classify(context.Background(), "148.163.139.74")

	if f.calls != 2 {
		t.Fatalf("reverse lookups = %d, want 2 — the entry should have expired", f.calls)
	}
}

// The suffix match decides whether a name is a scanner's, so a sloppy match is
// how an attacker's host gets treated as Proofpoint's — or, worse, how a real
// reader's does.
func TestMatchHost(t *testing.T) {
	machine := []string{
		"mx0b-00364e01.pphosted.com",
		"pphosted.com",
		"crawl-66-249-66-1.googlebot.com",
		"something.protection.outlook.com",
	}
	for _, h := range machine {
		if _, ok := matchHost(h); !ok {
			t.Errorf("matchHost(%q) = false, want true", h)
		}
	}

	human := []string{
		// Gmail's proxy: a display proxy, judged by classifyFetch, not here.
		"google-proxy-66-249-84-1.google.com",
		"evil-google.com",
		"google.com.attacker.example",
		"notpphosted.com",
		"mail.example.com",
		"",
		"outlook.com",
	}
	for _, h := range human {
		if suffix, ok := matchHost(h); ok {
			t.Errorf("matchHost(%q) matched %q, want no match", h, suffix)
		}
	}
}

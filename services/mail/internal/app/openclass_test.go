package app

import (
	"strings"
	"testing"
	"time"
)

// The agents here are not invented. They are what the event log actually
// recorded for two tracked messages sent to a columbia.edu address that the
// recipient had not opened — which is what sent us looking in the first place.
func TestClassifyFetch(t *testing.T) {
	const scannerUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	const gmailProxyUA = "Mozilla/5.0 (Windows NT 5.1; rv:11.0) Gecko Firefox/11.0 (via ggpht.com GoogleImageProxy)"
	const realBrowserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36"

	cases := []struct {
		name      string
		ua        string
		to        string
		sinceSent time.Duration
		machine   bool
	}{
		{
			name:      "gateway scan seconds after sending",
			ua:        scannerUA,
			to:        "fy2272@columbia.edu",
			sinceSent: 28 * time.Second,
			machine:   true,
		},
		{
			name:      "same agent long after sending is not judged on timing alone",
			ua:        scannerUA,
			to:        "fy2272@columbia.edu",
			sinceSent: 6 * time.Hour,
			machine:   false,
		},
		{
			// The bug this file was rewritten for. columbia.edu publishes
			// Proofpoint MX records and delivers into Google Workspace behind
			// them, so its readers open mail in Gmail and the fetch arrives
			// through Google's proxy. Filtering it reported 未检测到打开 for a
			// message the recipient had plainly read.
			name:      "gmail proxy counts even when the domain looks nothing like google",
			ua:        gmailProxyUA,
			to:        "fy2272@columbia.edu",
			sinceSent: 3 * time.Hour,
			machine:   false,
		},
		{
			name:      "gmail proxy for a consumer gmail recipient",
			ua:        gmailProxyUA,
			to:        "buyer@gmail.com",
			sinceSent: 3 * time.Hour,
			machine:   false,
		},
		{
			name:      "yahoo's proxy is a display proxy too",
			ua:        "YahooMailProxy; https://help.yahoo.com/kb/yahoo-mail-proxy-SLN28749.html",
			to:        "buyer@yahoo.com",
			sinceSent: 3 * time.Hour,
			machine:   false,
		},
		{
			// A display proxy is exempt from the agent list, not from timing.
			// Providers do occasionally prefetch on delivery.
			name:      "a display proxy fetching on delivery is still suppressed",
			ua:        gmailProxyUA,
			to:        "fy2272@columbia.edu",
			sinceSent: 10 * time.Second,
			machine:   true,
		},
		{
			name:      "a person on a real browser well after sending counts",
			ua:        realBrowserUA,
			to:        "buyer@example.com",
			sinceSent: 90 * time.Minute,
			machine:   false,
		},
		{
			name:      "a person is not counted inside the scan window",
			ua:        realBrowserUA,
			to:        "buyer@example.com",
			sinceSent: 20 * time.Second,
			machine:   true,
		},
		{
			name:      "no user-agent at all",
			ua:        "",
			to:        "buyer@example.com",
			sinceSent: 2 * time.Hour,
			machine:   true,
		},
		{
			name:      "whitespace-only user-agent is no user-agent",
			ua:        "   ",
			to:        "buyer@example.com",
			sinceSent: 2 * time.Hour,
			machine:   true,
		},
		{
			name:      "named gateway",
			ua:        "Proofpoint-Sandbox/1.0",
			to:        "buyer@example.com",
			sinceSent: 2 * time.Hour,
			machine:   true,
		},
		{
			name:      "headless browser",
			ua:        "Mozilla/5.0 HeadlessChrome/120.0.0.0 Safari/537.36",
			to:        "buyer@example.com",
			sinceSent: 2 * time.Hour,
			machine:   true,
		},
		{
			name:      "plain http client",
			ua:        "curl/8.4.0",
			to:        "buyer@example.com",
			sinceSent: 2 * time.Hour,
			machine:   true,
		},
		{
			name:      "unknown send time skips the timing rule rather than guessing",
			ua:        realBrowserUA,
			to:        "buyer@example.com",
			sinceSent: 0,
			machine:   false,
		},
		{
			name:      "a clock skewed backwards must not be read as an instant open",
			ua:        realBrowserUA,
			to:        "buyer@example.com",
			sinceSent: -5 * time.Second,
			machine:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyFetch(tc.ua, tc.sinceSent)
			if got.machine != tc.machine {
				t.Fatalf("machine = %v, want %v (reason %q)", got.machine, tc.machine, got.reason)
			}
			if got.machine && got.reason == "" {
				t.Fatal("a fetch rejected as machine must say which rule rejected it")
			}
			if !got.machine && got.reason != "" {
				t.Fatalf("a fetch that counted should carry no reason, got %q", got.reason)
			}
		})
	}
}

// The address arrives in a header anyone can set, and it is written to an INET
// column whose insert error is discarded — so anything that is not an address
// has to be dropped here or the entire event disappears.
func TestSanitiseIP(t *testing.T) {
	cases := map[string]string{
		"203.0.113.7":                   "203.0.113.7",
		"  203.0.113.7  ":               "203.0.113.7",
		"2001:db8::1":                   "2001:db8::1",
		"203.0.113.7:44310":             "203.0.113.7",
		"[2001:db8::1]:44310":           "2001:db8::1",
		"":                              "",
		"   ":                           "",
		"unknown":                       "",
		"not an ip":                     "",
		"203.0.113.999":                 "",
		"'); DROP TABLE email_events--": "",
	}
	for in, want := range cases {
		if got := sanitiseIP(in); got != want {
			t.Errorf("sanitiseIP(%q) = %q, want %q", in, got, want)
		}
	}
}

// A User-Agent is unbounded and attacker-controlled.
func TestClampAgent(t *testing.T) {
	if got := clampAgent("Mozilla/5.0"); got != "Mozilla/5.0" {
		t.Errorf("a normal agent was altered: %q", got)
	}
	long := strings.Repeat("A", 5000)
	if got := clampAgent(long); len(got) != 400 {
		t.Errorf("length = %d, want 400", len(got))
	}
}

package app

import "testing"

// A "domain" field invites a whole address, and the only symptom is a
// malformed Message-ID that nobody notices until deliverability drops.
func TestDomainNormalisation(t *testing.T) {
	cases := map[string]string{
		"fy2272@columbia.edu": "columbia.edu",
		"columbia.edu":        "columbia.edu",
		"  sunrise.com  ":     "sunrise.com",
		"":                    "",
	}
	for in, want := range cases {
		got := normaliseDomain(in)
		if got != want {
			t.Errorf("normaliseDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

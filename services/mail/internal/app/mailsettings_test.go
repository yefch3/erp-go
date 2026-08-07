package app

import (
	"errors"
	"fmt"
	"testing"
)

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

// The attempt budget exists to protect our address's standing with the mail
// host, so it must only be spent on failures that actually reached the host.
// Everything this service decides on its own — no address, no host configured,
// a stored code it cannot decrypt — never left the network, and charging those
// meant an internal fault answered the next five attempts with 429 instead of
// the real reason.
func TestOnlyTheMailHostsRefusalIsMarkedAsSuch(t *testing.T) {
	host := hostRejected{errors.New("Invalid credentials (Failure)")}
	if !FromMailHost(host) {
		t.Error("a refusal from the host was not recognised as one")
	}
	if !FromMailHost(fmt.Errorf("verify: %w", host)) {
		t.Error("wrapping lost the fact that the host was what refused")
	}

	for _, ours := range []error{
		errors.New("请输入邮箱地址"),
		ErrMailHostNotConfigured,
		ErrNoKey,
		ErrNoMailAccount,
		nil,
	} {
		if FromMailHost(ours) {
			t.Errorf("%v was billed to the caller, but it never reached the host", ours)
		}
	}
}

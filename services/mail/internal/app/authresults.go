package app

import "strings"

// parseAuthResults pulls the two authenticated identities out of an
// Authentication-Results header: who the mail was *mailed by* (SPF) and who
// *signed* it (DKIM).
//
// These are the two lines Gmail shows under 「显示原始邮件」, and they answer a
// question the From header cannot: From is typed by the sender and can say
// anything, while these two were checked against DNS by the receiving server.
//
// Only pass verdicts are reported. A failed or absent check is not evidence of
// anything and must not be dressed as an identity — "signed by nobody" is the
// honest answer, and the screen says 未验证 rather than naming a domain the
// signature did not actually establish.
//
// Trust note: this header is written by whichever server handled the mail, and
// an upstream one can write whatever it likes. The topmost is the one our own
// host added, which is why the caller passes the first header rather than
// joining them all — see firstHeader.
func parseAuthResults(v string) (spfDomain, dkimDomain string) {
	for _, part := range strings.Split(v, ";") {
		part = strings.TrimSpace(part)
		lower := strings.ToLower(part)
		switch {
		case strings.HasPrefix(lower, "dkim=pass"):
			// header.d is the signing domain proper; header.i is the signing
			// identity, whose domain half means the same thing when d is
			// absent. Gmail displays exactly this.
			if d := valueAfter(part, "header.d="); d != "" {
				dkimDomain = domainOf(d)
			} else if i := valueAfter(part, "header.i="); i != "" {
				dkimDomain = domainOf(i)
			}
		case strings.HasPrefix(lower, "spf=pass"):
			// The envelope sender, not the From: SPF authorises the machine
			// that handed the mail over, and the address it did so on behalf
			// of is smtp.mailfrom.
			if m := valueAfter(part, "smtp.mailfrom="); m != "" {
				spfDomain = domainOf(m)
			} else if hf := valueAfter(part, "smtp.helo="); hf != "" {
				spfDomain = domainOf(hf)
			}
		}
	}
	return spfDomain, dkimDomain
}

// valueAfter returns the token following key, stopping at whitespace.
//
// Skips anything inside parentheses first: SPF results carry a human-readable
// comment — "(google.com: domain of x designates 1.2.3.4 as permitted sender)"
// — and that comment routinely contains the very substrings being searched for.
func valueAfter(s, key string) string {
	s = stripParens(s)
	i := strings.Index(strings.ToLower(s), key)
	if i < 0 {
		return ""
	}
	rest := s[i+len(key):]
	if j := strings.IndexAny(rest, " \t\r\n;"); j >= 0 {
		rest = rest[:j]
	}
	return strings.Trim(rest, `"'`)
}

func stripParens(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
				continue
			}
			b.WriteRune(r)
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// domainOf takes the domain half of an address, or the value itself when it is
// already a bare domain. Leading @ is what header.i looks like.
func domainOf(v string) string {
	v = strings.TrimSpace(strings.Trim(v, `"'<>`))
	if i := strings.LastIndex(v, "@"); i >= 0 {
		v = v[i+1:]
	}
	return strings.ToLower(strings.TrimSuffix(v, "."))
}

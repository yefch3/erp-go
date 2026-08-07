package app

import (
	"strings"
	"unicode"

	"github.com/sgao19/erp-go/pkg/apierr"
)

// What makes a password acceptable.
//
// This file exists because of a trade-off made in the rate limiter next door.
// The lockout window was cut from fifteen minutes to sixty seconds so that
// somebody being attacked is shut out for a minute rather than a quarter of an
// hour — but a shorter lock means more guesses per hour get through, and
// ERPNext, whose shape that change copies, only affords a loose limit because
// it ships password scoring switched on by default.
//
// So the two move together. Loosening the limit without this would be a strict
// downgrade, and shipping only one half of the pair is the kind of change that
// looks like hardening and is not.
//
// What is deliberately NOT here: composition rules. Requiring an upper-case
// letter, a digit and a symbol is how you get "P@ssw0rd1" — it converts the
// whole workforce onto the same handful of predictable shapes, which is the
// opposite of the goal. Length, plus refusing the passwords attackers actually
// try, does more.

const (
	// Ten, not eight. Eight is the floor NIST sets for a system that also
	// rate-limits hard; ours now rate-limits gently, so the password carries
	// more of the weight.
	//
	// Measured in effective length rather than characters — see
	// effectiveLength, which exists because a character-count minimum quietly
	// punishes the language most of this company writes in.
	minPasswordLen = 10
	// And a floor on actual characters, whatever alphabet they come from. Five
	// hanzi carry plenty of theoretical entropy and are also how you write a
	// four-word phrase everybody knows.
	minPasswordRunes = 6
	// No upper bound worth enforcing below this. A long passphrase is the
	// best password most people will ever choose, and a maximum length is a
	// reason not to use one. This exists only so a megabyte of text cannot be
	// pushed through argon2.
	maxPasswordLen = 256
)

// commonPasswords is the list attackers work through first.
//
// Weighted towards what is actually tried against a Chinese company rather
// than a generic English top-100: keyboard walks, birthday-shaped digits,
// 拼音 endearments, and the "admin/test/1qaz" family every scanner carries.
// A short curated list catches most real attempts; the tail is long and adds
// little, which is why this is a compiled-in set rather than a data file
// somebody has to remember to ship.
var commonPasswords = map[string]bool{
	"password": true, "passwd": true, "pass": true, "admin": true, "administrator": true,
	"root": true, "test": true, "guest": true, "user": true, "login": true,
	"welcome": true, "letmein": true, "monkey": true, "dragon": true, "master": true,
	"qwerty": true, "qwertyuiop": true, "asdfgh": true, "asdfghjkl": true, "zxcvbn": true,
	"1qaz2wsx": true, "1q2w3e4r": true, "1q2w3e": true, "qazwsx": true, "qweasd": true,
	"123456": true, "1234567": true, "12345678": true, "123456789": true, "1234567890": true,
	"111111": true, "000000": true, "666666": true, "888888": true, "123123": true,
	"abc123": true, "a123456": true, "123abc": true, "abcd1234": true, "qwe123": true,
	"woaini": true, "woaini1314": true, "5201314": true, "1314520": true, "wangyi": true,
	"iloveyou": true, "sunshine": true, "princess": true, "football": true, "baseball": true,
	"changeme": true, "secret": true, "default": true, "temp": true, "demo": true,
}

// checkPasswordStrength refuses what a person will regret choosing.
//
// context is whatever the attacker already knows about this account — the
// address, the person's name, the company domain. Those are the first things
// tried and the first things people reach for, so a password built out of them
// is the worst of both.
func checkPasswordStrength(password string, context ...string) error {
	runes := []rune(password)
	if len(runes) < minPasswordRunes || effectiveLength(runes) < minPasswordLen {
		return apierr.Invalid("IAM_PASSWORD_TOO_SHORT", "密码太短了，至少 10 位（中文可以短一些）")
	}
	if len(runes) > maxPasswordLen {
		return apierr.Invalid("IAM_PASSWORD_TOO_LONG", "密码太长了，请控制在 256 位以内")
	}
	if isCommonPassword(password) || walksTheKeyboard(password) {
		return apierr.Invalid("IAM_PASSWORD_TOO_COMMON",
			"这是最常被猜到的密码之一，换一个")
	}
	if isRepetitive(runes) {
		return apierr.Invalid("IAM_PASSWORD_TOO_SIMPLE",
			"密码太规律了（重复或连续的字符），换一个")
	}
	if who := borrowsFromIdentity(password, context); who != "" {
		return apierr.Invalid("IAM_PASSWORD_FROM_IDENTITY",
			"密码里不要包含「"+who+"」——猜密码的人第一个就试这个")
	}
	return nil
}

// effectiveLength weighs a character by how much choosing it narrows things
// down.
//
// A Latin letter is one of twenty-six; a hanzi is one of several thousand in
// common use. Counting them the same means "我今天想吃小笼包" — eight
// characters, and a far better password than anything of eight Latin letters —
// gets refused while "aaaaaaaaaab" passes. That is the wrong way round, and it
// lands on exactly the people this system is for.
//
// Two, not the ~2.4 the entropy ratio would justify. The number is a floor
// being set, not a measurement being reported, and rounding it down keeps this
// from becoming a way to pass with three characters.
func effectiveLength(runes []rune) int {
	n := 0
	for _, r := range runes {
		if r > unicode.MaxASCII {
			n += 2
			continue
		}
		n++
	}
	return n
}

// keyboardWalks are the paths a finger takes when somebody is not choosing a
// password so much as declining to.
//
// Checked as substrings rather than listed as literals, because the family is
// generated, not enumerated: 1q2w3e4r, 1q2w3e4r5t, q1w2e3 and a dozen more are
// all the same two rows being zipped together, and a fixed list is a game of
// catch-up that the list always loses.
var keyboardWalks = []string{
	"1234567890", "0987654321",
	"qwertyuiop", "poiuytrewq",
	"asdfghjkl", "lkjhgfdsa",
	"zxcvbnm", "mnbvcxz",
	"1qaz2wsx3edc4rfv5tgb6yhn7ujm",
	"1q2w3e4r5t6y7u8i9o0p",
	"q1w2e3r4t5y6u7i8o9p0",
	"!@#$%^&*()",
}

// walksTheKeyboard reports whether the whole password is a slice of one of
// those paths.
func walksTheKeyboard(password string) bool {
	lower := strings.ToLower(password)
	if len([]rune(lower)) < 6 {
		return false
	}
	for _, walk := range keyboardWalks {
		if strings.Contains(walk, lower) {
			return true
		}
	}
	return false
}

// isCommonPassword strips the decoration people add to a bad password and
// checks what is underneath.
//
// "Password" and "password123!" are the same password wearing different hats,
// and an attacker's list contains both forms. Checking only the literal string
// would let every one of them through.
func isCommonPassword(password string) bool {
	base := strings.ToLower(strings.TrimSpace(password))
	if commonPasswords[base] {
		return true
	}
	// Trailing digits and punctuation: the "add a 1 and a bang" reflex.
	stripped := strings.TrimRight(base, "0123456789!@#$%^&*_-.")
	if stripped != base && commonPasswords[stripped] {
		return true
	}
	// Leading decoration too, less common but the same idea.
	return commonPasswords[strings.Trim(base, "0123456789!@#$%^&*_-.")]
}

// isRepetitive catches "aaaaaaaaaa" and "abcdefghij" — strings long enough to
// pass the length check while containing almost no choice.
//
// Only runs of six or more, so an ordinary password with "789" or "aaa" inside
// it is untouched. The test is on the whole string being one run, not on any
// run existing.
func isRepetitive(runes []rune) bool {
	same, ascending, descending := 1, 1, 1
	for i := 1; i < len(runes); i++ {
		switch {
		case runes[i] == runes[i-1]:
			same++
		default:
			same = 1
		}
		switch {
		case runes[i] == runes[i-1]+1:
			ascending++
		default:
			ascending = 1
		}
		switch {
		case runes[i] == runes[i-1]-1:
			descending++
		default:
			descending = 1
		}
		if same >= 6 || ascending >= 6 || descending >= 6 {
			return true
		}
	}
	return false
}

// borrowsFromIdentity reports which piece of the person's own identity the
// password contains, or "" if none.
//
// Returns the offending word rather than a boolean so the message can name it.
// "密码太弱" sends somebody away guessing; "不要包含「zhangsan」" is a thing
// they can act on in one try.
func borrowsFromIdentity(password string, context []string) string {
	lower := strings.ToLower(password)
	for _, raw := range context {
		for _, part := range identityParts(raw) {
			// Four is the shortest fragment worth refusing. Below that,
			// ordinary words collide with initials and short surnames, and
			// refusing an otherwise good password over "sun" would teach
			// nothing except that this system is annoying.
			if len(part) >= 4 && strings.Contains(lower, part) {
				return part
			}
		}
	}
	return ""
}

// identityParts breaks an address or a name into the fragments somebody would
// actually build a password out of: the part before the @, each label of the
// domain, and each run of letters in a name.
func identityParts(raw string) []string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return nil
	}
	var out []string
	for _, chunk := range strings.FieldsFunc(raw, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		// Public suffixes are shared by everyone and carry no information
		// about this person; refusing a password for containing "com" would
		// be noise.
		switch chunk {
		case "com", "cn", "net", "org", "edu", "gov", "www", "mail", "email":
			continue
		}
		out = append(out, chunk)
	}
	return out
}

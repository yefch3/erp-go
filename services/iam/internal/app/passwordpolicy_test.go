package app

import (
	"strings"
	"testing"
	"time"
)

func reason(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		return ""
	}
	return err.Error()
}

// The list is not decoration. The rate limit was loosened from fifteen minutes
// to sixty seconds in the same change, which lets roughly six hundred guesses
// an hour through — harmless against a password worth having, and a week's
// work against "Password2024".
func TestTheCommonPasswordsAreRefused(t *testing.T) {
	for _, p := range []string{
		"password12", "Password123", "password2024!", "qwertyuiop",
		"1234567890", "a123456789", "woaini1314", "admin12345", "1q2w3e4r5t",
	} {
		if err := checkPasswordStrength(p); err == nil {
			t.Fatalf("%q was accepted", p)
		}
	}
}

// Decoration is what people add when a system tells them their password is too
// weak, and it is what an attacker's list already contains. Checking the
// literal string only would let every one of these through.
func TestDressingUpABadPasswordDoesNotSaveIt(t *testing.T) {
	for _, p := range []string{"password!!", "PASSWORD123", "!password!", "monkey1234"} {
		if err := checkPasswordStrength(p); err == nil {
			t.Fatalf("%q was accepted", p)
		}
	}
}

// The first guess anybody makes against zhangsan@aaaindustryinc.com is
// something containing "zhangsan". The message names the offending word,
// because "密码太弱" sends somebody away guessing and this does not.
func TestAPasswordBuiltFromTheOwnIdentityIsRefused(t *testing.T) {
	ctx := []string{"zhangsan@aaaindustryinc.com", "张三", "E100"}
	for _, p := range []string{
		"zhangsan2024", "MyZhangSanPass", "aaaindustryinc9", "xxzhangsanxx",
	} {
		err := checkPasswordStrength(p, ctx...)
		if err == nil {
			t.Fatalf("%q was accepted", p)
		}
		if !strings.Contains(reason(t, err), "不要包含") {
			t.Fatalf("%q was refused for the wrong reason: %v", p, err)
		}
	}
}

// Public suffixes belong to everybody and say nothing about this person.
// Refusing a good password for containing "com" would be pure noise.
func TestThePublicPartOfADomainIsNotIdentity(t *testing.T) {
	if err := checkPasswordStrength("comcomcomcast-purple", "zhangsan@aaaindustryinc.com"); err != nil {
		t.Fatalf("a password was refused over a public suffix: %v", err)
	}
}

// Below four characters, ordinary words collide with initials and short
// surnames. Refusing an otherwise fine password over "sun" teaches nothing
// except that this system is annoying.
func TestVeryShortIdentityFragmentsAreIgnored(t *testing.T) {
	if err := checkPasswordStrength("sunset-marmalade", "sun@aaaindustryinc.com", "孙"); err != nil {
		t.Fatalf("a password was refused over a three-letter fragment: %v", err)
	}
}

// Long enough to pass the length check, almost no choice inside it.
func TestRunsAndSequencesAreRefused(t *testing.T) {
	for _, p := range []string{"aaaaaaaaaaaa", "abcdefghijkl", "zyxwvutsrqpo", "111111111111"} {
		if err := checkPasswordStrength(p); err == nil {
			t.Fatalf("%q was accepted", p)
		}
	}
}

// A run inside a password is not a run of a password. Refusing anything
// containing "789" would fail most passphrases anybody actually types.
func TestAShortRunInsideAGoodPasswordIsFine(t *testing.T) {
	for _, p := range []string{"purple-horse789-staple", "tram22canal", "abc-mango-delta"} {
		if err := checkPasswordStrength(p); err != nil {
			t.Fatalf("%q was refused: %v", p, err)
		}
	}
}

// No composition rules, on purpose. Requiring an upper-case letter, a digit
// and a symbol is how a whole workforce ends up on "P@ssw0rd1" — the same
// handful of predictable shapes, which is the opposite of the goal. A long
// ordinary passphrase has to pass.
func TestAPlainLongPassphrasePasses(t *testing.T) {
	for _, p := range []string{
		"correct horse battery staple",
		"我今天想吃小笼包",
		"tram-window-copper-lemon",
	} {
		if err := checkPasswordStrength(p, "zhangsan@aaaindustryinc.com", "张三"); err != nil {
			t.Fatalf("%q was refused: %v", p, err)
		}
	}
}

func TestLengthIsMeasuredInCharactersNotBytes(t *testing.T) {
	// Ten Chinese characters is thirty bytes. Measuring bytes would let a
	// four-character password through and refuse nothing anybody noticed.
	if err := checkPasswordStrength("小笼包蟹粉"); err == nil {
		t.Fatal("a five-character password was accepted")
	}
}

// The floor moved from eight to ten in the same change that shortened the
// lockout window, and the two numbers only make sense together. A future
// change that loosens one without the other should have to look at this.
func TestTheFloorMatchesTheLooserRateLimit(t *testing.T) {
	if minPasswordLen < 10 {
		t.Fatalf("minimum length is %d; the sixty-second window assumes at least 10", minPasswordLen)
	}
}

// A maximum exists only so a megabyte of text cannot be pushed through
// argon2, and it has to be far above any passphrase somebody would choose.
func TestTheMaximumOnlyStopsAbuse(t *testing.T) {
	if maxPasswordLen < 128 {
		t.Fatalf("maximum length is %d, which is low enough to discourage a passphrase", maxPasswordLen)
	}
	if err := checkPasswordStrength(strings.Repeat("好", maxPasswordLen+1)); err == nil {
		t.Fatal("an oversized password was accepted")
	}
}

// The window and the message have to agree: a sixty-second lock reported as
// "1 分钟" reads as much worse news than it is, and the whole point of the
// short window is that this is an interruption rather than an outage.
func TestAShortWaitIsReportedInSeconds(t *testing.T) {
	msg := errTooManyAttempts(40 * time.Second).Error()
	if !strings.Contains(msg, "秒后重试") {
		t.Fatalf("a forty-second wait was reported as %q", msg)
	}
	long := errTooManyAttempts(10 * time.Minute).Error()
	if !strings.Contains(long, "分钟后重试") {
		t.Fatalf("a ten-minute wait was reported as %q", long)
	}
}

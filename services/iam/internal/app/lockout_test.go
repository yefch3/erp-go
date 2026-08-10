package app

import (
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
)

// The message is the fix.
//
// What it replaces — "账号已锁定，请联系管理员" — was true and useless twice
// over: there was nobody to contact who could do anything about it, and no
// amount of waiting helped either, because the lock never expired.
func TestTheLockoutSaysWhenItEnds(t *testing.T) {
	for _, wait := range []time.Duration{time.Second, 30 * time.Second, 14 * time.Minute} {
		msg := errTooManyAttempts(wait).Error()
		if !strings.Contains(msg, "后重试") {
			t.Fatalf("wait %v gave %q, which does not tell anybody when to come back", wait, msg)
		}
	}
}

// Rounded up, so the advice is never early. Told "40 秒" at 41 seconds
// remaining, somebody comes back to the same refusal.
func TestTheWaitIsRoundedUpNeverDown(t *testing.T) {
	cases := map[time.Duration]string{
		1 * time.Second:                       "2 秒",
		40*time.Second + 500*time.Millisecond: "41 秒",
		2 * time.Minute:                       "3 分钟",
		14*time.Minute + 59*time.Second:       "15 分钟",
	}
	for wait, want := range cases {
		if msg := errTooManyAttempts(wait).Error(); !strings.Contains(msg, want) {
			t.Fatalf("with %v left: got %q, want it to contain %q", wait, msg, want)
		}
	}
}

// "Later" and "no" are different answers, and anything counting rejected
// credentials has to be able to tell them apart. The gateway's failure budget
// charges Unauthenticated; if being told to wait were Unauthenticated, waiting
// would spend the budget that waiting exists to restore.
func TestBeingThrottledIsNotTheSameAsBeingRejected(t *testing.T) {
	var throttled *apierr.Error
	if !asAPIError(errTooManyAttempts(time.Minute), &throttled) {
		t.Fatal("the throttle error is not a business error")
	}
	if throttled.Kind != apierr.KindThrottled {
		t.Fatalf("kind is %v, want KindThrottled", throttled.Kind)
	}
	if errBadCredentials.Kind == apierr.KindThrottled {
		t.Fatal("a wrong password now reads as a throttle")
	}
}

func asAPIError(err error, target **apierr.Error) bool {
	e, ok := err.(*apierr.Error)
	if ok {
		*target = e
	}
	return ok
}

// namedQuery returns one sqlc query's SQL, comments stripped.
//
// Sliced on the next "-- name:" rather than on a semicolon, because the
// comments above these queries are prose and prose has semicolons in it —
// which is exactly how this helper was wrong the first time, quietly reporting
// that a clause was missing when it was three lines further down.
func namedQuery(t *testing.T, name string) string {
	t.Helper()
	sql := readSource(t, "../../db/queries/iam.sql")
	start := strings.Index(sql, "-- name: "+name+" ")
	if start < 0 {
		t.Fatalf("query %q not found", name)
	}
	rest := sql[start:]
	if next := strings.Index(rest[1:], "\n-- name: "); next >= 0 {
		rest = rest[:next+1]
	}
	var body []string
	for _, line := range strings.Split(rest, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			body = append(body, line)
		}
	}
	return strings.Join(body, "\n")
}

// The lock is consulted before the password is verified, and that ordering is
// the whole difference between a rate limit and a differently-worded refusal.
// Check the password first and guessing continues at full speed.
//
// Note this is the opposite of the ordering ActivateAccount needs, and for the
// opposite reason: there the token is cheap to check and the hash is the
// expensive part to protect; here the lock is the thing being enforced.
func TestTheLockIsCheckedBeforeThePasswordIs(t *testing.T) {
	body := between(t, readSource(t, "service.go"), "func (s *Service) Login", "\n}\n")
	lock := strings.Index(body, "u.LockedUntil.Valid")
	verify := strings.Index(body, "VerifyPassword(u.PasswordHash")
	if lock < 0 || verify < 0 {
		t.Fatal("Login no longer both checks the lock and verifies a password")
	}
	if verify < lock {
		t.Fatal("the password is verified before the lock is consulted, so the lock stops nothing")
	}
}

// An administrator clicking 重置密码 on a locked account used to get
// {"reset":true} and change nothing that person could feel: the query cleared
// failed_count and left the lock alone. A new password is a stronger statement
// than waiting out a timer, so it has to clear it.
func TestResettingAPasswordUnlocksTheAccount(t *testing.T) {
	if stmt := namedQuery(t, "UpdatePassword"); !strings.Contains(stmt, "locked_until = NULL") {
		t.Fatal("an administrator's password reset leaves the account locked")
	}
}

// Signing in successfully clears the deadline too, not just the counter — a
// stale timestamp would refuse the next attempt from somebody who just proved
// they know the password.
func TestSigningInClearsTheDeadline(t *testing.T) {
	if stmt := namedQuery(t, "RecordLoginSuccess"); !strings.Contains(stmt, "locked_until = NULL") {
		t.Fatal("a successful sign-in leaves the lock behind")
	}
}

// Guessing must not renew the lock it put on somebody else. Without the
// greatest(), the sixth wrong password pushes a fifteen-minute wait out to
// thirty, and the attacker holds the door shut for as long as they keep
// typing — the punishment landing entirely on the account's owner.
func TestGuessingCannotExtendTheLock(t *testing.T) {
	if stmt := namedQuery(t, "RecordLoginFailure"); !strings.Contains(stmt, "greatest(locked_until") {
		t.Fatal("a later failure can push the deadline further out")
	}
}

// 'LOCKED' is gone from users.status, and must stay gone. The column now means
// one thing — what an administrator decided — and the only reason the old
// value was unrecoverable is that a login failure could write a permanent
// state into a column nothing else ever reset.
func TestAFailedLoginCannotWriteAPermanentState(t *testing.T) {
	sql := readSource(t, "../../db/queries/iam.sql")
	if strings.Contains(sql, "'LOCKED'") {
		t.Fatal("a query still writes or reads the permanent LOCKED status")
	}
	if strings.Contains(readSource(t, "service.go"), `"LOCKED"`) {
		t.Fatal("Login still branches on the permanent LOCKED status")
	}
}

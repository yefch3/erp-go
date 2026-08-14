package app

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sgao19/erp-go/services/iam/internal/store"
)

func readSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	// 统一换行符，确保同一份源码在 Windows 和 CI/Linux 上执行相同的结构断言。
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func between(t *testing.T, src, start, end string) string {
	t.Helper()
	i := strings.Index(src, start)
	if i < 0 {
		t.Fatalf("%q not found", start)
	}
	rest := src[i:]
	j := strings.Index(rest, end)
	if j < 0 {
		t.Fatalf("%q not found after %q", end, start)
	}
	return rest[:j]
}

func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

var now = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// goodInvitation is a link that works, so each test can spoil exactly one
// thing and the failure names itself.
func goodInvitation() store.GetInvitationByTokenRow {
	return store.GetInvitationByTokenRow{
		ID: 7, TenantID: 1, EmployeeID: 42,
		Email:        "alice@aaaindustryinc.com",
		CurrentEmail: "alice@aaaindustryinc.com",
		ExpiresAt:    ts(now.Add(24 * time.Hour)),
		EmployeeName: "李爱丽", EmployeeStatus: "ACTIVE", TenantStatus: "ACTIVE",
	}
}

func TestAGoodLinkIsUsable(t *testing.T) {
	if err := invitationFault(goodInvitation(), now); err != nil {
		t.Fatalf("a link with nothing wrong with it was refused: %v", err)
	}
}

func TestEveryReasonALinkFailsIsToldApart(t *testing.T) {
	// The messages are the product here: somebody reading them has no other
	// channel to ask what went wrong, and "try again" is useless advice for
	// three of the four.
	cases := []struct {
		name  string
		spoil func(*store.GetInvitationByTokenRow)
		want  error
	}{
		{"已经用过", func(i *store.GetInvitationByTokenRow) { i.UsedAt = ts(now.Add(-time.Hour)) }, errActivateUsed},
		{"已过期", func(i *store.GetInvitationByTokenRow) { i.ExpiresAt = ts(now.Add(-time.Second)) }, errActivateExpired},
		{"地址被改过", func(i *store.GetInvitationByTokenRow) { i.CurrentEmail = "bob@aaaindustryinc.com" }, errActivateAddressChanged},
		{"人已离职", func(i *store.GetInvitationByTokenRow) { i.EmployeeStatus = "LEFT" }, errActivateNotEmployable},
		{"公司被停用", func(i *store.GetInvitationByTokenRow) { i.TenantStatus = "SUSPENDED" }, errActivateNotEmployable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inv := goodInvitation()
			c.spoil(&inv)
			err := invitationFault(inv, now)
			if !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
}

// The reason this check exists at all. Without it an administrator could
// invite somebody, edit the employee row to their own address before the
// invitation is opened, and have the victim's click stamp the administrator's
// address as verified on the victim's account.
func TestEditingTheAddressAfterSendingKillsTheLink(t *testing.T) {
	inv := goodInvitation()
	inv.CurrentEmail = "attacker@aaaindustryinc.com"
	if err := invitationFault(inv, now); !errors.Is(err, errActivateAddressChanged) {
		t.Fatalf("a link whose target moved was accepted: %v", err)
	}
}

func TestTheAddressComparisonIgnoresCaseAndPadding(t *testing.T) {
	// employees.email is edited by hand and " Alice@..." is what hands
	// produce. Refusing a good link over a space would be indistinguishable
	// from the tampering case above, and far more common.
	inv := goodInvitation()
	inv.CurrentEmail = "  Alice@AAAindustryinc.com "
	if err := invitationFault(inv, now); err != nil {
		t.Fatalf("a link was refused over case and whitespace: %v", err)
	}
}

func TestALinkExpiresExactlyAtItsDeadline(t *testing.T) {
	inv := goodInvitation()
	inv.ExpiresAt = ts(now)
	if err := invitationFault(inv, now); err != nil {
		t.Fatalf("the deadline instant itself was refused: %v", err)
	}
	if err := invitationFault(inv, now.Add(time.Nanosecond)); !errors.Is(err, errActivateExpired) {
		t.Fatalf("a link past its deadline was accepted: %v", err)
	}
}

// An account activated by some other route while this link was in flight.
// From the person's side the account works, so "already used" is the true and
// useful thing to say — not an internal state name.
func TestAnAlreadyVerifiedAccountReadsAsAlreadyUsed(t *testing.T) {
	inv := goodInvitation()
	inv.EmailVerifiedAt = ts(now.Add(-time.Minute))
	if err := invitationFault(inv, now); !errors.Is(err, errActivateUsed) {
		t.Fatalf("got %v, want %v", err, errActivateUsed)
	}
}

// A suspended company and a departed employee deliberately answer the same
// thing. The caller is unauthenticated; telling them which of the two it is
// says more about the company than they asked.
func TestASuspendedCompanyAndADepartedEmployeeAnswerAlike(t *testing.T) {
	left, suspended := goodInvitation(), goodInvitation()
	left.EmployeeStatus = "LEFT"
	suspended.TenantStatus = "SUSPENDED"
	a, b := invitationFault(left, now), invitationFault(suspended, now)
	if a.Error() != b.Error() {
		t.Fatalf("the two answers differ: %q vs %q", a, b)
	}
}

// The window is a product decision, not an accident of arithmetic: long
// enough for somebody on leave, short enough that live keys do not pile up.
func TestTheInvitationWindowIsSevenDays(t *testing.T) {
	if invitationTTL != 7*24*time.Hour {
		t.Fatalf("invitation window is %v", invitationTTL)
	}
}

// The token is guarded by entropy and nothing else: the route that redeems it
// cannot be rate limited by identity, because there is no identity yet.
func TestTheTokenIsBigEnoughToBeUnguessable(t *testing.T) {
	if invitationTokenBytes < 32 {
		t.Fatalf("token is %d bytes, want at least 32", invitationTokenBytes)
	}
}

// Storage holds sha256(token) and never the token. This asserts on the source
// because the property is about what is absent — no path that writes a raw
// token into a column — and no runtime call can show an absence.
func TestTheRawTokenIsNeverStored(t *testing.T) {
	src := readSource(t, "invitation.go")
	if !strings.Contains(src, "sha256.Sum256([]byte(token))") {
		t.Fatal("the token is no longer hashed before lookup")
	}
	if strings.Contains(src, "TokenHash: []byte(token)") {
		t.Fatal("a raw token is being written to storage")
	}
}

// argon2id is 64 MiB and several milliseconds per call, on a route anybody can
// reach without an account. Hashing before the token is known to be good would
// make sixteen concurrent requests cost a gigabyte.
func TestABadTokenNeverReachesTheHasher(t *testing.T) {
	src := readSource(t, "invitation.go")
	body := between(t, src, "func (s *Service) ActivateAccount", "\n}\n")
	check := strings.Index(body, "s.usableInvitation(")
	hash := strings.Index(body, "HashPassword(")
	if check < 0 || hash < 0 {
		t.Fatal("ActivateAccount no longer both validates and hashes")
	}
	if hash < check {
		t.Fatal("the password is hashed before the token is validated")
	}
}

package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/services/iam/internal/store"
)

// The fault matrix for a reset link, exercised without a database — the same
// discipline as the invitation tests beside this file: passwordResetFault was
// handed the clock precisely so that "expired" does not take an hour to test.

// goodReset is a link that works, so each test can spoil exactly one thing
// and the failure names itself.
func goodReset() store.GetPasswordResetByTokenRow {
	return store.GetPasswordResetByTokenRow{
		ID: 9, TenantID: 1, EmployeeID: 42,
		Email:          "alice@aaaindustryinc.com",
		CurrentEmail:   "alice@aaaindustryinc.com",
		EmployeeName:   "Alice",
		ExpiresAt:      ts(now.Add(30 * time.Minute)),
		TenantStatus:   "ACTIVE",
		EmployeeStatus: "ACTIVE",
	}
}

func TestAGoodResetLinkPasses(t *testing.T) {
	if err := passwordResetFault(goodReset(), now); err != nil {
		t.Fatalf("a good link was refused: %v", err)
	}
}

func TestAUsedResetLinkNamesItself(t *testing.T) {
	row := goodReset()
	row.UsedAt = ts(now.Add(-time.Minute))
	if err := passwordResetFault(row, now); !errors.Is(err, errResetUsed) {
		t.Fatalf("want errResetUsed, got %v", err)
	}
}

func TestAnExpiredResetLinkNamesItself(t *testing.T) {
	row := goodReset()
	row.ExpiresAt = ts(now.Add(-time.Second))
	if err := passwordResetFault(row, now); !errors.Is(err, errResetExpired) {
		t.Fatalf("want errResetExpired, got %v", err)
	}
}

func TestAResetLinkDiesWithAnAddressChange(t *testing.T) {
	// The token proves control of the mailbox it was SENT to. If the record's
	// address moved since, redeeming would hand the account to a mailbox that
	// never earned it — the same drift rule as activation.
	row := goodReset()
	row.CurrentEmail = "attacker@aaaindustryinc.com"
	if err := passwordResetFault(row, now); !errors.Is(err, errResetAddressChanged) {
		t.Fatalf("want errResetAddressChanged, got %v", err)
	}
}

func TestAResetLinkDiesWhenThePersonLeaves(t *testing.T) {
	row := goodReset()
	row.EmployeeStatus = "INACTIVE"
	if err := passwordResetFault(row, now); !errors.Is(err, errResetUnavailable) {
		t.Fatalf("want errResetUnavailable, got %v", err)
	}
}

func TestAResetLinkDiesWithItsTenant(t *testing.T) {
	row := goodReset()
	row.TenantStatus = "SUSPENDED"
	if err := passwordResetFault(row, now); !errors.Is(err, errResetUnavailable) {
		t.Fatalf("want errResetUnavailable, got %v", err)
	}
}

// The structural promise the whole design rests on: the administrator paths
// never store a password without raising must_change_password, and the
// owner-chosen paths clear it. Asserted against the source the same way the
// lockout tests assert Login's structure — a regression here is a silent
// security hole, not a failing feature.
func TestAdminTypedPasswordsMustBeChanged(t *testing.T) {
	src := readSource(t, "account.go")
	openAccount := between(t, src, "func (s *Service) OpenAccount", "\n}\n")
	if !strings.Contains(openAccount, "MustChange: true") {
		t.Error("OpenAccount stores an admin-typed password without raising must_change_password")
	}
	reset := between(t, src, "func (s *Service) ResetPassword", "\n}\n")
	if !strings.Contains(reset, "MustChange: true") {
		t.Error("ResetPassword stores an admin-typed password without raising must_change_password")
	}
	change := between(t, src, "func (s *Service) ChangePassword", "\n}\n")
	if !strings.Contains(change, "MustChange: false") {
		t.Error("ChangePassword does not clear must_change_password for an owner-chosen password")
	}
	redeem := between(t, readSource(t, "passwordreset.go"),
		"func (s *Service) RedeemPasswordReset", "\n}\n")
	if !strings.Contains(redeem, "MustChange: false") {
		t.Error("RedeemPasswordReset does not clear must_change_password for an owner-chosen password")
	}
	if !strings.Contains(redeem, "ClearLoginLock") {
		t.Error("RedeemPasswordReset leaves a stale login lock waiting on the OLD password")
	}
}

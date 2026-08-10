package grpcx

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
)

// The key has to exist before anything in this package touches it, and it is
// read once for the life of the process — so it is set here rather than in
// each test.
func TestMain(m *testing.M) {
	if err := os.Setenv(signingKeyEnv, strings.Repeat("test-key-", 8)); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// signed builds the metadata a caller would send.
func signed(op Operator, method string, at time.Time) metadata.MD {
	ts := at.Unix()
	return metadata.Pairs(
		mdTimestamp, strconv.FormatInt(ts, 10),
		mdSignature, sign(op, ts, method),
	)
}

var alice = Operator{TenantID: 1, EmployeeID: 42, Name: "李娜"}

func TestAProperlySignedCallIsAccepted(t *testing.T) {
	now := time.Now()
	if err := verify(signed(alice, "/erp.iam.v1.Iam/GetUser", now), alice,
		"/erp.iam.v1.Iam/GetUser", now); err != nil {
		t.Fatalf("a call we signed ourselves was refused: %v", err)
	}
}

// The whole point. Before this, x-employee-id: 1 arrived as a plain header and
// was believed — anything that reached the port was the administrator of every
// tenant.
func TestAnUnsignedCallIsRefused(t *testing.T) {
	md := metadata.Pairs(mdTenantID, "1", mdEmployeeID, "1")
	if err := verify(md, Operator{TenantID: 1, EmployeeID: 1}, "/x/Y", time.Now()); err == nil {
		t.Fatal("an unsigned call was accepted")
	}
}

// Claiming to be somebody else means the claims no longer match what was
// signed. This is the attack the change exists to stop.
func TestClaimingADifferentIdentityBreaksTheSignature(t *testing.T) {
	now := time.Now()
	md := signed(alice, "/x/Y", now)
	impostor := Operator{TenantID: 1, EmployeeID: 1, Name: "系统管理员"}
	if err := verify(md, impostor, "/x/Y", now); err == nil {
		t.Fatal("a forged identity was accepted")
	}
}

// Every field that is signed has to matter, or it is decoration. The name in
// particular: it is displayed, logged, and written into the export audit.
func TestEveryClaimIsCovered(t *testing.T) {
	now := time.Now()
	md := signed(alice, "/x/Y", now)
	for name, tampered := range map[string]Operator{
		"tenant":   {TenantID: 2, EmployeeID: 42, Name: "李娜"},
		"employee": {TenantID: 1, EmployeeID: 43, Name: "李娜"},
		"name":     {TenantID: 1, EmployeeID: 42, Name: "张三"},
	} {
		if err := verify(md, tampered, "/x/Y", now); err == nil {
			t.Errorf("changing the %s was accepted", name)
		}
	}
}

// A signature captured from a harmless call must not be usable on a
// destructive one. Without the method in the payload, the same claims would
// validate anywhere.
func TestASignatureDoesNotTransferToAnotherMethod(t *testing.T) {
	now := time.Now()
	md := signed(alice, "/erp.mail.v1.Emails/GetInbound", now)
	if err := verify(md, alice, "/erp.iam.v1.Iam/ResetPassword", now); err == nil {
		t.Fatal("a signature for one method was accepted on another")
	}
}

// The window bounds how long a captured signature stays useful.
func TestAnOldSignatureExpires(t *testing.T) {
	issued := time.Now().Add(-signatureWindow - time.Minute)
	md := signed(alice, "/x/Y", issued)
	if err := verify(md, alice, "/x/Y", time.Now()); err == nil {
		t.Fatal("a signature from outside the window was accepted")
	}
}

// And a timestamp from the future is refused too — otherwise a caller could
// mint one good for as long as they liked.
func TestASignatureFromTheFutureIsRefused(t *testing.T) {
	issued := time.Now().Add(signatureWindow + time.Minute)
	md := signed(alice, "/x/Y", issued)
	if err := verify(md, alice, "/x/Y", time.Now()); err == nil {
		t.Fatal("a signature dated in the future was accepted")
	}
}

// Moving the clock forward must not let an old signature back in by changing
// what it covers: the timestamp is signed, so it cannot be edited.
func TestTheTimestampItselfCannotBeEdited(t *testing.T) {
	now := time.Now()
	old := now.Add(-signatureWindow - time.Minute)
	md := signed(alice, "/x/Y", old)
	// Same signature, timestamp rewritten to now.
	md.Set(mdTimestamp, strconv.FormatInt(now.Unix(), 10))
	if err := verify(md, alice, "/x/Y", now); err == nil {
		t.Fatal("rewriting the timestamp revived an expired signature")
	}
}

// Background work — mailbox sync, the image cache, the backfills — calls other
// services with nobody logged in. Those calls are signed over an empty
// identity; if they were left unsigned instead, every background path would be
// the unauthenticated one.
func TestCallsWithNobodyLoggedInAreStillSigned(t *testing.T) {
	now := time.Now()
	nobody := Operator{}
	if err := verify(signed(nobody, "/x/Y", now), nobody, "/x/Y", now); err != nil {
		t.Fatalf("an unattended background call was refused: %v", err)
	}
	// And an attacker cannot borrow that shape to become somebody.
	if err := verify(signed(nobody, "/x/Y", now), alice, "/x/Y", now); err == nil {
		t.Fatal("an empty-identity signature validated a real identity")
	}
}

func TestGarbageInsteadOfASignatureIsRefused(t *testing.T) {
	now := time.Now()
	for name, md := range map[string]metadata.MD{
		"empty signature": metadata.Pairs(mdTimestamp, strconv.FormatInt(now.Unix(), 10), mdSignature, ""),
		"not hex":         metadata.Pairs(mdTimestamp, strconv.FormatInt(now.Unix(), 10), mdSignature, "zzzz"),
		"missing time":    metadata.Pairs(mdSignature, sign(alice, now.Unix(), "/x/Y")),
		"time is not a number": metadata.Pairs(mdTimestamp, "yesterday",
			mdSignature, sign(alice, now.Unix(), "/x/Y")),
		"nothing at all": metadata.MD{},
	} {
		if err := verify(md, alice, "/x/Y", now); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// The reason never reaches the caller — see unaryOperator, which logs it and
// answers Unauthenticated. This checks the reasons are at least distinct in
// the log, because "your clock is wrong" and "your key is wrong" are very
// different things to be told at 2am.
func TestTheReasonsAreDistinguishableInTheLog(t *testing.T) {
	now := time.Now()
	unsigned := verify(metadata.MD{}, alice, "/x/Y", now)
	stale := verify(signed(alice, "/x/Y", now.Add(-time.Hour)), alice, "/x/Y", now)
	wrong := verify(signed(alice, "/x/Y", now), Operator{EmployeeID: 9}, "/x/Y", now)
	if unsigned == nil || stale == nil || wrong == nil {
		t.Fatal("one of these should have failed")
	}
	if unsigned.Error() == stale.Error() || stale.Error() == wrong.Error() {
		t.Fatalf("indistinguishable: %v / %v / %v", unsigned, stale, wrong)
	}
}

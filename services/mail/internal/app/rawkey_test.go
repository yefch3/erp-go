package app

import "testing"

// Where one message's original MIME lives.
//
// The key was tenant/account/uid, on the assumption that a UID identifies a
// message within a mailbox. It does not, and one real mailbox paid for it with
// 88 shared objects across 176 messages: a mail the person sent on the 1st was
// overwritten by a spam filed on the 6th, and the row went on pointing at it
// as though it were the original.
func TestRawKeySeparatesFoldersThatShareAUID(t *testing.T) {
	const (
		tenant, account = int64(1), int64(1)
		validity, uid   = uint32(9), uint32(614)
	)
	inbox := rawKeyFor(tenant, account, "INBOX", validity, uid)
	sent := rawKeyFor(tenant, account, "SENT", validity, uid)
	junk := rawKeyFor(tenant, account, "JUNK", validity, uid)

	if inbox == sent || inbox == junk || sent == junk {
		t.Fatalf("UID %d in three folders shares an object:\n  INBOX %s\n  SENT  %s\n  JUNK  %s\n"+
			"a UID is unique within a folder, not within an account - whichever "+
			"synced last would overwrite the rest", uid, inbox, sent, junk)
	}
}

// A UID only means anything while UIDVALIDITY holds. When a host renumbers -
// a migration, a mailbox rebuild - the sync starts over, and without the
// validity in the key the new numbering writes over the old generation.
func TestRawKeySeparatesUIDGenerations(t *testing.T) {
	before := rawKeyFor(1, 1, "INBOX", 9, 614)
	after := rawKeyFor(1, 1, "INBOX", 10, 614)
	if before == after {
		t.Errorf("UID 614 before and after a UIDVALIDITY change share %s: "+
			"the renumbered message overwrites the original it replaced", before)
	}
}

func TestRawKeySeparatesAccountsAndTenants(t *testing.T) {
	base := rawKeyFor(1, 1, "INBOX", 9, 614)
	if other := rawKeyFor(1, 2, "INBOX", 9, 614); other == base {
		t.Error("two accounts share an object")
	}
	if other := rawKeyFor(2, 1, "INBOX", 9, 614); other == base {
		t.Error("two tenants share an object")
	}
}

// The same inputs must always name the same object, or a repair pass looking
// for a message's original would not find the one ingest wrote.
func TestRawKeyIsStable(t *testing.T) {
	a := rawKeyFor(1, 1, "INBOX", 9, 614)
	b := rawKeyFor(1, 1, "INBOX", 9, 614)
	if a != b {
		t.Errorf("%s != %s", a, b)
	}
	if want := "mail/inbound/1/1/INBOX/9/614.eml"; a != want {
		t.Errorf("key = %s, want %s\n"+
			"the shape is not arbitrary: mail/inbound/ is the prefix the "+
			"campaign attachment guard refuses, and changing it silently "+
			"orphans every original already written", a, want)
	}
}

-- +goose Up
-- The address this message actually left from, stamped at send time.
--
-- Until now the sent view resolved a sender's address by joining
-- mail_accounts on employee_id — that is, by asking "what mailbox is this
-- person bound to *right now*". The query carried a comment admitting the
-- flaw and betting it would not matter:
--
--     Reads today's binding, so a mailbox rebound since the send would show
--     the new address. […] not worth it until somebody actually rebinds
--     mid-history.
--
-- Somebody rebound mid-history. An account moved from one address to another
-- and back, and every message sent from the intermediate address immediately
-- began reporting the current one as its sender. Not a missing value — a
-- confident, wrong value, on a screen whose entire job is to say what was
-- sent to whom. The same class of mistake as deriving `tracked` from the
-- stored body (migration 00019), and the fix is the same: record the fact at
-- the moment it is true, because nothing afterwards can reconstruct it.
ALTER TABLE email_messages
    ADD COLUMN IF NOT EXISTS from_email VARCHAR(320) NOT NULL DEFAULT '';

-- No backfill.
--
-- Every candidate source is a guess. Today's binding is exactly the wrong
-- answer this column exists to stop repeating; the HR record is a profile
-- field that need never have matched the mailbox; and reconstructing from
-- sync history only covers the sends whose host copy still exists, which is
-- precisely the set that did not need this column.
--
-- So history stays empty and the screen says the address was not recorded.
-- An admitted gap is worth more than a plausible invention: a person reading
-- "未记录" goes and checks, a person reading the wrong address does not.
--
-- +goose Down
ALTER TABLE email_messages DROP COLUMN IF EXISTS from_email;

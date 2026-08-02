-- +goose Up

-- Inbox housekeeping: star, archive, trash.
--
-- All three are ERP-side state, deliberately not written back to the mail
-- host over IMAP: the sync direction stays one-way (host → us), which is
-- what keeps a sync bug from ever being able to damage the real mailbox.
-- is_read already works this way.
--
-- Archive and trash are timestamps, not booleans: "when" costs nothing now
-- and is exactly the question somebody asks later. Trash is soft-only —
-- there is no hard-delete path, the raw MIME in object storage stays.
ALTER TABLE email_inbound ADD COLUMN is_starred  BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE email_inbound ADD COLUMN archived_at TIMESTAMPTZ;
ALTER TABLE email_inbound ADD COLUMN deleted_at  TIMESTAMPTZ;

-- +goose Down
ALTER TABLE email_inbound DROP COLUMN is_starred;
ALTER TABLE email_inbound DROP COLUMN archived_at;
ALTER TABLE email_inbound DROP COLUMN deleted_at;

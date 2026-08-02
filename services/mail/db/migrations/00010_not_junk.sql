-- +goose Up

-- "Not spam": rescue a mis-flagged mail from the junk view into the inbox.
--
-- A flag rather than rewriting `folder`: the row's identity is
-- (account, folder, imap_uid), and moving it into INBOX could collide with a
-- real inbox UID. The flag changes where the mail SHOWS without touching
-- what it IS — and, like all housekeeping here, nothing is written back to
-- the mail host.
ALTER TABLE email_inbound ADD COLUMN not_junk BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE email_inbound DROP COLUMN not_junk;

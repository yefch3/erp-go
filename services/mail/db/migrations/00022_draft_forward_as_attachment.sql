-- +goose Up
-- Whether a saved draft was forwarding its original as a .eml attachment.
--
-- The draft already remembers what it forwards (forward_inbound_id); this is
-- how it forwards it, and the two are not the same choice. Without it, saving
-- an attachment-forward and reopening it silently turns it into a quoted
-- forward — the same failure C22 fixed for send mode, CC and reply context,
-- where a saved reply came back as a plain new mail. A draft has to reopen as
-- the message it was, not as its words.
ALTER TABLE email_drafts
    ADD COLUMN IF NOT EXISTS forward_as_attachment BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE email_drafts DROP COLUMN IF EXISTS forward_as_attachment;

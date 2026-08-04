-- +goose Up
-- Which ERP send this Sent-folder copy is the host's copy of.
--
-- The host keeps its own copy of everything sent over SMTP, including what
-- the ERP sent. Until now ingest recognised those and threw them away, so an
-- ERP send existed only as a delivery record — a row in email_messages with
-- no message behind it. That record cannot be starred, archived or deleted,
-- because those act on a message in a folder and there was no such message.
--
-- Now the copy is kept and linked back instead. The Sent folder becomes what
-- it should be: real messages, all of them, every mailbox action working the
-- same way it does in the inbox. The delivery record supplies what only it
-- knows — per-recipient status, and whether the tracking pixel was fetched.
--
-- 0 rather than NULL for "not one of ours", matching how the rest of this
-- schema spells absent ids, and so the column can stay NOT NULL.
ALTER TABLE email_inbound
    ADD COLUMN IF NOT EXISTS sent_message_id BIGINT NOT NULL DEFAULT 0;

-- The Sent list joins on this for every row it shows.
CREATE INDEX IF NOT EXISTS email_inbound_sent_msg_idx
    ON email_inbound (tenant_id, sent_message_id)
    WHERE sent_message_id <> 0;

-- +goose Down
DROP INDEX IF EXISTS email_inbound_sent_msg_idx;
ALTER TABLE email_inbound DROP COLUMN IF EXISTS sent_message_id;

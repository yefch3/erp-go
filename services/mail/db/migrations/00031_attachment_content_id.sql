-- +goose Up

-- The number a mail uses to point at a picture it is carrying.
--
-- A signature logo is not fetched from anywhere: it travels inside the message
-- as an attachment part, and the body points at it with
-- <img src="cid:8E971712-2A98-45B3-B6A2-3F14F66CD9AD">. "cid" is Content-ID,
-- an identifier that means something only within that one message.
--
-- Ingest stored the part and threw the identifier away. So the picture was
-- sitting in object storage the whole time and nothing connected it to the
-- body that wanted it: the browser met a src it has never understood, drew the
-- broken-image glyph, and printed the alt text. 116 messages in this mailbox
-- are affected, 572 pictures between them.
--
-- The same missing field caused a second, quieter problem. With no way to tell
-- an embedded logo from a real attachment, both were listed — so a signature
-- graphic appeared in the attachment row next to the signed contract. One
-- column fixes both.
ALTER TABLE email_inbound_attachments
    ADD COLUMN content_id VARCHAR(255) NOT NULL DEFAULT '';

-- Looked up once per message on open, to turn every cid: in the body into a
-- signed storage URL. Partial: most attachments are real attachments and
-- carry no Content-ID at all.
CREATE INDEX email_inbound_att_cid_idx
    ON email_inbound_attachments (tenant_id, inbound_id, content_id)
    WHERE content_id <> '';

-- No backfill here. The identifiers are recoverable — the original MIME of
-- every message is still in object storage, which is what it is kept for —
-- but recovering them means re-parsing a few hundred messages, and a
-- migration is the wrong place to read from a bucket. RunContentIDBackfill
-- does it on start-up instead, and finds its own work: a message qualifies
-- while its body names a cid: that none of its attachments answers to, so the
-- pass stops looking at a message the moment it is fixed and needs no marker
-- column to remember that.

-- +goose Down
DROP INDEX IF EXISTS email_inbound_att_cid_idx;
ALTER TABLE email_inbound_attachments DROP COLUMN IF EXISTS content_id;

-- +goose Up

-- A draft used to hold only subject, body, recipients and attachments, so
-- saving a half-written reply and coming back to it produced a plain new
-- mail: the send mode reverted to separate, the CC list vanished, and the
-- threading headers that make it show up as a reply in the customer's client
-- were gone. The person could not see any of that had happened — the text
-- was all there.
--
-- These four columns are the rest of what a compose actually is.
ALTER TABLE email_drafts
    -- SEPARATE: one copy each, invisible to one another. MERGED: one shared
    -- mail with an open To and CC.
    ADD COLUMN send_mode VARCHAR(16) NOT NULL DEFAULT 'SEPARATE'
        CHECK (send_mode IN ('SEPARATE','MERGED')),
    -- Same shape as recipients: [{contactId, name, email, customerId, ...}].
    -- Only meaningful in merged mode, which is the only mode where a CC has
    -- a meaning at all.
    ADD COLUMN cc JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- What this draft answers or forwards. Plain ids rather than a foreign
    -- key: the referenced mail can be purged from the trash while a draft
    -- about it is still open, and that should cost the reply its threading,
    -- not block the deletion or break the draft.
    ADD COLUMN reply_to_inbound_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN forward_inbound_id BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE email_drafts
    DROP COLUMN send_mode,
    DROP COLUMN cc,
    DROP COLUMN reply_to_inbound_id,
    DROP COLUMN forward_inbound_id;

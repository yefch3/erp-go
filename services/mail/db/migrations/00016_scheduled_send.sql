-- +goose Up

-- Timed delivery. The queue already knows how to hold a message back — the
-- worker claims what is due, and a retry sets a future next_retry_at — so the
-- schedule is one more column rather than a scheduler.
--
-- Deliberately not folded into next_retry_at: "waiting because the person
-- asked" and "waiting because the last attempt failed" look identical in one
-- column, and the 已定时 list would then have to guess which is which.
ALTER TABLE email_messages  ADD COLUMN scheduled_at TIMESTAMPTZ;
ALTER TABLE email_campaigns ADD COLUMN scheduled_at TIMESTAMPTZ;

-- What the send was answering. The campaign already carries the body and the
-- recipients; without this, cancelling a scheduled reply and getting it back
-- as a draft would return a plain new mail — same words, different message.
--
-- Only the reply side. A forward's attachments were copied onto the campaign
-- when it was created, so restoring one from the forward id too would attach
-- every file twice.
ALTER TABLE email_campaigns ADD COLUMN reply_to_inbound_id BIGINT NOT NULL DEFAULT 0;

-- A cancelled message is not a failed one: nothing was attempted, nobody has
-- to look at it, and it must never be picked up by a retry sweep. It keeps its
-- row because the campaign it belonged to is still a record of what somebody
-- composed.
ALTER TABLE email_messages DROP CONSTRAINT email_messages_status_check;
ALTER TABLE email_messages ADD CONSTRAINT email_messages_status_check
    CHECK (status IN ('QUEUED','SENDING','ACCEPTED','DELIVERED','SEND_UNKNOWN',
                      'SOFT_BOUNCED','HARD_BOUNCED','COMPLAINED','FAILED',
                      'NEEDS_ATTENTION','CANCELLED'));

-- The 已定时 list, and the cancel/send-now lookups behind it.
CREATE INDEX email_messages_scheduled_idx
    ON email_messages (tenant_id, scheduled_at, campaign_id)
    WHERE status = 'QUEUED' AND scheduled_at IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS email_messages_scheduled_idx;
ALTER TABLE email_messages DROP CONSTRAINT email_messages_status_check;
ALTER TABLE email_messages ADD CONSTRAINT email_messages_status_check
    CHECK (status IN ('QUEUED','SENDING','ACCEPTED','DELIVERED','SEND_UNKNOWN',
                      'SOFT_BOUNCED','HARD_BOUNCED','COMPLAINED','FAILED',
                      'NEEDS_ATTENTION'));
ALTER TABLE email_campaigns DROP COLUMN reply_to_inbound_id;
ALTER TABLE email_campaigns DROP COLUMN scheduled_at;
ALTER TABLE email_messages  DROP COLUMN scheduled_at;

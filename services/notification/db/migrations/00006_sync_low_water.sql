-- +goose Up

-- The backfill low-water mark.
--
-- last_uid records the newest message we hold; this records the oldest. The
-- first sync deliberately starts near the top of the mailbox — the message
-- somebody is waiting for must not queue behind years of history — so history
-- is filled in from here downwards, a batch per pass, until the configured
-- cap. 0 means backfill has not started; 1 means the bottom was reached.
ALTER TABLE mail_sync_state ADD COLUMN low_uid BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE mail_sync_state DROP COLUMN low_uid;

-- +goose Up
ALTER TABLE shipping_arrival_reminders
    DROP CONSTRAINT shipping_arrival_reminders_status_check;
ALTER TABLE shipping_arrival_reminders
    ADD CONSTRAINT shipping_arrival_reminders_status_check
        CHECK (status IN ('PENDING','PROCESSING','SENT','CANCELLED','FAILED')),
    ADD COLUMN title TEXT NOT NULL DEFAULT '',
    ADD COLUMN content TEXT NOT NULL DEFAULT '',
    ADD COLUMN detail_url TEXT NOT NULL DEFAULT '',
    ADD COLUMN read_at TIMESTAMPTZ,
    ADD COLUMN attempt_count INT NOT NULL DEFAULT 0,
    ADD COLUMN last_error TEXT NOT NULL DEFAULT '',
    ADD COLUMN next_retry_at TIMESTAMPTZ;

CREATE INDEX shipping_arrival_reminders_inbox_idx
    ON shipping_arrival_reminders (tenant_id, recipient_employee_id, sent_at DESC)
    WHERE status = 'SENT';

DROP INDEX shipping_arrival_reminders_due_idx;
CREATE INDEX shipping_arrival_reminders_due_idx
    ON shipping_arrival_reminders (tenant_id, (COALESCE(next_retry_at, due_at)))
    WHERE status IN ('PENDING','FAILED');

ALTER TABLE shipping_schedule_changes
    DROP CONSTRAINT shipping_schedule_changes_change_type_check;
ALTER TABLE shipping_schedule_changes
    ADD CONSTRAINT shipping_schedule_changes_change_type_check
        CHECK (change_type IN ('DATE','ETA','STATUS','CANCEL','VESSEL_VOYAGE','ROUTE','TEMPORARY_CALL','PROGRESS','DELAY','DOCUMENT','REMINDER'));

-- +goose Down
ALTER TABLE shipping_schedule_changes
    DROP CONSTRAINT shipping_schedule_changes_change_type_check;
ALTER TABLE shipping_schedule_changes
    ADD CONSTRAINT shipping_schedule_changes_change_type_check
        CHECK (change_type IN ('DATE','ETA','STATUS','CANCEL','VESSEL_VOYAGE','ROUTE','TEMPORARY_CALL','PROGRESS','DELAY','DOCUMENT'));

DROP INDEX shipping_arrival_reminders_inbox_idx;
DROP INDEX shipping_arrival_reminders_due_idx;
ALTER TABLE shipping_arrival_reminders
    DROP COLUMN next_retry_at,
    DROP COLUMN last_error,
    DROP COLUMN attempt_count,
    DROP COLUMN read_at,
    DROP COLUMN detail_url,
    DROP COLUMN content,
    DROP COLUMN title,
    DROP CONSTRAINT shipping_arrival_reminders_status_check;
ALTER TABLE shipping_arrival_reminders
    ADD CONSTRAINT shipping_arrival_reminders_status_check
        CHECK (status IN ('PENDING','SENT','CANCELLED','FAILED'));
CREATE INDEX shipping_arrival_reminders_due_idx
    ON shipping_arrival_reminders (tenant_id, due_at) WHERE status = 'PENDING';

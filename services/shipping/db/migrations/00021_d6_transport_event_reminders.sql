-- +goose Up
ALTER TABLE shipping_arrival_reminders
    DROP CONSTRAINT shipping_arrival_reminders_reminder_type_check;
ALTER TABLE shipping_arrival_reminders
    ADD CONSTRAINT shipping_arrival_reminders_reminder_type_check
        CHECK (reminder_type ~ '^(ARRIVAL|DEPARTURE)_[0-9]+D$');

-- +goose Down
DELETE FROM shipping_arrival_reminders WHERE reminder_type LIKE 'DEPARTURE_%';
ALTER TABLE shipping_arrival_reminders
    DROP CONSTRAINT shipping_arrival_reminders_reminder_type_check;
ALTER TABLE shipping_arrival_reminders
    ADD CONSTRAINT shipping_arrival_reminders_reminder_type_check
        CHECK (reminder_type ~ '^ARRIVAL_[0-9]+D$');

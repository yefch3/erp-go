-- +goose Up
ALTER TABLE shipping_schedules
    ADD COLUMN booking_no TEXT NOT NULL DEFAULT '',
    ADD COLUMN bill_of_lading_no TEXT NOT NULL DEFAULT '',
    ADD COLUMN warehouse_entry_date DATE,
    ADD COLUMN customs_declaration_date DATE,
    ADD COLUMN freight_currency TEXT NOT NULL DEFAULT '',
    ADD COLUMN freight_amount TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE shipping_schedules
    DROP COLUMN freight_amount,
    DROP COLUMN freight_currency,
    DROP COLUMN customs_declaration_date,
    DROP COLUMN warehouse_entry_date,
    DROP COLUMN bill_of_lading_no,
    DROP COLUMN booking_no;

-- +goose Up
-- Cross-service references intentionally have no database foreign key: customer
-- and supplier master data live in the masterdata service database.  The name
-- columns remain snapshots so historical schedules do not change when a master
-- record is renamed.
ALTER TABLE shipping_schedules
    ADD COLUMN carrier_id BIGINT;

CREATE INDEX shipping_schedules_carrier_idx
    ON shipping_schedules (tenant_id, carrier_id)
    WHERE carrier_id IS NOT NULL;

-- +goose Down
DROP INDEX shipping_schedules_carrier_idx;
ALTER TABLE shipping_schedules DROP COLUMN carrier_id;

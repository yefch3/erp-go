-- +goose Up
ALTER TABLE customer_owners
    ADD COLUMN is_primary BOOLEAN NOT NULL DEFAULT false;

CREATE UNIQUE INDEX customer_owners_one_active_primary_idx
    ON customer_owners (tenant_id, customer_id)
    WHERE status = 'ACTIVE' AND is_primary = true;

-- +goose Down
DROP INDEX IF EXISTS customer_owners_one_active_primary_idx;
ALTER TABLE customer_owners DROP COLUMN IF EXISTS is_primary;

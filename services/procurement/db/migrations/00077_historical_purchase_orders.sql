-- +goose Up
CREATE TABLE IF NOT EXISTS historical_purchase_orders (
 tenant_id BIGINT NOT NULL,
 po_id BIGINT NOT NULL REFERENCES purchase_orders(id),
 original_date DATE NOT NULL,
 contact_name TEXT NOT NULL DEFAULT '',
 contact_phone TEXT NOT NULL DEFAULT '',
 prices_complete BOOLEAN NOT NULL DEFAULT false,
 recorded_by_id BIGINT NOT NULL,
 recorded_by_name TEXT NOT NULL,
 recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, po_id)
);
ALTER TABLE purchase_order_items ADD COLUMN IF NOT EXISTS price_missing BOOLEAN NOT NULL DEFAULT false;
-- +goose Down
ALTER TABLE purchase_order_items DROP COLUMN price_missing;
DROP TABLE historical_purchase_orders;

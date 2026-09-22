-- +goose Up
ALTER TABLE manual_shipping_orders
 ADD COLUMN linked_contract_id BIGINT NOT NULL DEFAULT 0,
 ADD COLUMN original_contract_no VARCHAR(50) NOT NULL DEFAULT '',
 ADD COLUMN linked_by BIGINT,
 ADD COLUMN linked_at TIMESTAMPTZ;
CREATE INDEX manual_shipping_contract_link_idx ON manual_shipping_orders(tenant_id,linked_contract_id) WHERE linked_contract_id>0;
-- +goose Down
DROP INDEX manual_shipping_contract_link_idx;
ALTER TABLE manual_shipping_orders DROP COLUMN linked_contract_id,DROP COLUMN original_contract_no,DROP COLUMN linked_by,DROP COLUMN linked_at;

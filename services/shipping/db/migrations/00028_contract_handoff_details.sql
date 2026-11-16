-- +goose Up
ALTER TABLE contract_shipping_handoffs ADD COLUMN presale_references_json text NOT NULL DEFAULT '', ADD COLUMN allocation_status text NOT NULL DEFAULT '';
-- +goose Down
ALTER TABLE contract_shipping_handoffs DROP COLUMN presale_references_json, DROP COLUMN allocation_status;

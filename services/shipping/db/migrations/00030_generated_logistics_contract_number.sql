-- +goose Up
CREATE SEQUENCE logistics_contract_number_seq;
ALTER TABLE contract_shipping_handoffs ADD COLUMN forwarder_contract_no_generated boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE contract_shipping_handoffs DROP COLUMN forwarder_contract_no_generated;
DROP SEQUENCE logistics_contract_number_seq;

-- +goose Up
ALTER TABLE contract_shipping_handoffs ADD COLUMN source_sales_remark TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE contract_shipping_handoffs DROP COLUMN source_sales_remark;

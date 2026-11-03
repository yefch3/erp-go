-- +goose Up
ALTER TABLE purchase_requirements ADD COLUMN contract_item_remark text NOT NULL DEFAULT '', ADD COLUMN presale_reference_json text NOT NULL DEFAULT '';
ALTER TABLE purchase_order_items ADD COLUMN contract_item_remark text NOT NULL DEFAULT '';
-- +goose Down
ALTER TABLE purchase_order_items DROP COLUMN contract_item_remark;
ALTER TABLE purchase_requirements DROP COLUMN contract_item_remark, DROP COLUMN presale_reference_json;

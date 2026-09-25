-- +goose Up
ALTER TABLE purchase_requirements ADD COLUMN source_sales_remark TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE purchase_requirements DROP COLUMN source_sales_remark;

-- +goose Up
ALTER TABLE quotations ADD COLUMN source_sales_remark TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE quotations DROP COLUMN source_sales_remark;

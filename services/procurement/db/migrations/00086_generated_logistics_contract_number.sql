-- +goose Up
ALTER TABLE purchase_orders ADD COLUMN business_document_no_generated boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE purchase_orders DROP COLUMN business_document_no_generated;

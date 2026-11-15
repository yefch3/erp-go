-- +goose Up
ALTER TABLE purchase_orders ADD COLUMN transport_batch_no integer NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE purchase_orders DROP COLUMN transport_batch_no;

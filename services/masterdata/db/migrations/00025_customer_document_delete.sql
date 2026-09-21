-- +goose Up
ALTER TABLE customer_documents ADD COLUMN deleted_at TIMESTAMPTZ;
-- +goose Down
ALTER TABLE customer_documents DROP COLUMN deleted_at;

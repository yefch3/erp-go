-- +goose Up
ALTER TABLE contracts ADD COLUMN approval_request_key TEXT NOT NULL DEFAULT '';
ALTER TABLE contracts ADD COLUMN approval_instance_id BIGINT NOT NULL DEFAULT 0;
-- +goose Down
ALTER TABLE contracts DROP COLUMN approval_request_key, DROP COLUMN approval_instance_id;

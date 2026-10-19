-- +goose Up
ALTER TABLE contract_attachments ADD COLUMN signature_invalidated_at timestamptz;

-- +goose Down
ALTER TABLE contract_attachments DROP COLUMN signature_invalidated_at;

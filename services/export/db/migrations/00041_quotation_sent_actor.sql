-- +goose Up
ALTER TABLE quotations ADD COLUMN sent_by BIGINT;

-- +goose Down
ALTER TABLE quotations DROP COLUMN sent_by;

-- +goose Up
ALTER TABLE purchase_requirements
    ADD COLUMN execution_rfq_deleted_at timestamptz,
    ADD COLUMN execution_rfq_deleted_by bigint;

-- +goose Down
ALTER TABLE purchase_requirements
    DROP COLUMN execution_rfq_deleted_by,
    DROP COLUMN execution_rfq_deleted_at;

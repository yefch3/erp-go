-- +goose Up
-- migration-safety: additive snapshot column is backward compatible; historical selections keep an empty specification.
ALTER TABLE sourcing_customer_selection_items ADD COLUMN product_spec TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sourcing_customer_selection_items DROP COLUMN product_spec;

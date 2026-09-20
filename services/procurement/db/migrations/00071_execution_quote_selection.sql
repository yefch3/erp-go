-- +goose Up
-- The buyer's final choice is business data, not browser state.  Keep it on
-- the quote so reopening an inquiry shows the same decision and records who
-- made it.  At most one quote may be selected for a requirement.
ALTER TABLE purchase_execution_supplier_quotes
  ADD COLUMN selected BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN selected_by_id BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN selected_by_name VARCHAR(100) NOT NULL DEFAULT '',
  ADD COLUMN selected_at TIMESTAMPTZ;

CREATE UNIQUE INDEX purchase_execution_supplier_quotes_one_selected_idx
  ON purchase_execution_supplier_quotes (tenant_id, requirement_id)
  WHERE selected;

-- +goose Down
DROP INDEX purchase_execution_supplier_quotes_one_selected_idx;
ALTER TABLE purchase_execution_supplier_quotes
  DROP COLUMN selected_at,
  DROP COLUMN selected_by_name,
  DROP COLUMN selected_by_id,
  DROP COLUMN selected;

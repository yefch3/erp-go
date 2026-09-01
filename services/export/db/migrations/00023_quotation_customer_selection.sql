-- +goose Up
-- migration-safety: nullable provenance columns and a partial unique index are backward compatible with existing quotation writers.
ALTER TABLE quotations ADD COLUMN source_customer_selection_id BIGINT;
ALTER TABLE quotations ADD COLUMN source_customer_selection_no VARCHAR(40) NOT NULL DEFAULT '';
ALTER TABLE quotations ADD COLUMN source_customer_selection_version INTEGER NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX quotations_customer_selection_uidx
  ON quotations(tenant_id, source_customer_selection_id)
  WHERE source_customer_selection_id IS NOT NULL AND status <> 'CANCELLED';

-- +goose Down
DROP INDEX quotations_customer_selection_uidx;
ALTER TABLE quotations DROP COLUMN source_customer_selection_version;
ALTER TABLE quotations DROP COLUMN source_customer_selection_no;
ALTER TABLE quotations DROP COLUMN source_customer_selection_id;

-- +goose Up
ALTER TABLE purchase_execution_supplier_quotes
  ADD COLUMN quote_category VARCHAR(32) NOT NULL DEFAULT '',
  ADD COLUMN incoterm TEXT NOT NULL DEFAULT '',
  ADD COLUMN calculated_unit_price NUMERIC(20,6),
  ADD COLUMN calculation_input JSONB NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN calculated_at TIMESTAMPTZ,
  ADD COLUMN calculated_by_id BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN calculated_by_name VARCHAR(100) NOT NULL DEFAULT '';
ALTER TABLE purchase_execution_supplier_quotes ADD CONSTRAINT purchase_execution_supplier_quotes_category_check CHECK (quote_category IN ('','FOB_USD','FOB_CNY','ALL_IN_PORT_CNY','EX_FACTORY_CNY','REPROCESSING_CNY','DIRECT_CFR_USD'));
CREATE TABLE purchase_execution_inquiry_files (
  id BIGSERIAL PRIMARY KEY, tenant_id BIGINT NOT NULL, requirement_id BIGINT NOT NULL REFERENCES purchase_requirements(id),
  file_name TEXT NOT NULL, content_type TEXT NOT NULL DEFAULT 'application/octet-stream', size_bytes BIGINT NOT NULL,
  file_data BYTEA NOT NULL, uploaded_by_id BIGINT NOT NULL, uploaded_by_name VARCHAR(100) NOT NULL DEFAULT '', uploaded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX purchase_execution_inquiry_files_requirement_idx ON purchase_execution_inquiry_files(tenant_id,requirement_id,uploaded_at DESC);
CREATE TABLE purchase_order_execution_files (
  tenant_id BIGINT NOT NULL, po_id BIGINT NOT NULL REFERENCES purchase_orders(id) ON DELETE CASCADE,
  file_id BIGINT NOT NULL REFERENCES purchase_execution_inquiry_files(id) ON DELETE CASCADE,
  linked_at TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY (po_id,file_id)
);
CREATE INDEX purchase_order_execution_files_tenant_idx ON purchase_order_execution_files(tenant_id,po_id);
ALTER TABLE purchase_order_items
  ADD COLUMN execution_quote_id BIGINT REFERENCES purchase_execution_supplier_quotes(id);
CREATE INDEX purchase_order_items_execution_quote_idx ON purchase_order_items(tenant_id,execution_quote_id) WHERE execution_quote_id IS NOT NULL;
CREATE TABLE purchase_order_draft_files (
  id BIGSERIAL PRIMARY KEY, tenant_id BIGINT NOT NULL,
  po_id BIGINT NOT NULL REFERENCES purchase_orders(id) ON DELETE CASCADE,
  file_name TEXT NOT NULL, content_type TEXT NOT NULL DEFAULT 'application/octet-stream',
  size_bytes BIGINT NOT NULL, file_data BYTEA NOT NULL,
  uploaded_by_id BIGINT NOT NULL, uploaded_by_name VARCHAR(100) NOT NULL DEFAULT '',
  uploaded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX purchase_order_draft_files_order_idx ON purchase_order_draft_files(tenant_id,po_id,uploaded_at DESC);
-- +goose Down
DROP TABLE purchase_order_draft_files;
DROP INDEX purchase_order_items_execution_quote_idx;
ALTER TABLE purchase_order_items DROP COLUMN execution_quote_id;
DROP TABLE purchase_order_execution_files;
DROP TABLE purchase_execution_inquiry_files;
ALTER TABLE purchase_execution_supplier_quotes DROP CONSTRAINT purchase_execution_supplier_quotes_category_check, DROP COLUMN calculated_by_name, DROP COLUMN calculated_by_id, DROP COLUMN calculated_at, DROP COLUMN calculation_input, DROP COLUMN calculated_unit_price, DROP COLUMN incoterm, DROP COLUMN quote_category;

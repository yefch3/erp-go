-- +goose Up
-- D4 keeps the accepted purchase-order and outgoing-payment surfaces, while
-- adding the gates between final requote, signed supplier/forwarder contract,
-- Finance verification and payment request.
ALTER TABLE purchase_orders DROP CONSTRAINT purchase_orders_tenant_id_po_no_key;
CREATE UNIQUE INDEX purchase_orders_document_party_idx
  ON purchase_orders (tenant_id, po_no, supplier_id);

ALTER TABLE purchase_orders
  ADD COLUMN business_type VARCHAR(16) NOT NULL DEFAULT 'PROCUREMENT'
    CHECK (business_type IN ('PROCUREMENT','LOGISTICS','MANUAL')),
  ADD COLUMN source_business_id BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN export_contract_no VARCHAR(50) NOT NULL DEFAULT '',
  ADD COLUMN business_document_no VARCHAR(80) NOT NULL DEFAULT '',
  ADD COLUMN payment_terms TEXT NOT NULL DEFAULT '',
  ADD COLUMN signed_contract_key TEXT NOT NULL DEFAULT '',
  ADD COLUMN signed_contract_name TEXT NOT NULL DEFAULT '',
  ADD COLUMN signed_contract_uploaded_at TIMESTAMPTZ,
  ADD COLUMN contract_verified_at TIMESTAMPTZ,
  ADD COLUMN contract_verified_by BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN contract_verified_by_name VARCHAR(100) NOT NULL DEFAULT '',
  ADD COLUMN payment_requested_at TIMESTAMPTZ,
  ADD COLUMN payment_requested_by BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN payment_requested_by_name VARCHAR(100) NOT NULL DEFAULT '';

UPDATE purchase_orders SET business_type='MANUAL' WHERE supplier_id=0;
CREATE UNIQUE INDEX purchase_orders_external_source_idx
  ON purchase_orders (tenant_id, business_type, source_business_id)
  WHERE source_business_id<>0 AND business_type='LOGISTICS';
CREATE INDEX purchase_orders_payment_queue_idx
  ON purchase_orders (tenant_id, business_type, payment_requested_at DESC)
  WHERE payment_requested_at IS NOT NULL OR business_type='MANUAL';

-- +goose Down
DROP INDEX purchase_orders_payment_queue_idx;
DROP INDEX purchase_orders_external_source_idx;
ALTER TABLE purchase_orders
  DROP COLUMN payment_requested_by_name,
  DROP COLUMN payment_requested_by,
  DROP COLUMN payment_requested_at,
  DROP COLUMN contract_verified_by_name,
  DROP COLUMN contract_verified_by,
  DROP COLUMN contract_verified_at,
  DROP COLUMN signed_contract_uploaded_at,
  DROP COLUMN signed_contract_name,
  DROP COLUMN signed_contract_key,
  DROP COLUMN payment_terms,
  DROP COLUMN business_document_no,
  DROP COLUMN export_contract_no,
  DROP COLUMN source_business_id,
  DROP COLUMN business_type;
DROP INDEX purchase_orders_document_party_idx;
ALTER TABLE purchase_orders ADD CONSTRAINT purchase_orders_tenant_id_po_no_key UNIQUE (tenant_id, po_no);

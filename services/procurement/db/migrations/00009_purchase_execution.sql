-- +goose Up

ALTER TABLE purchase_orders
  ADD COLUMN send_status VARCHAR(24) NOT NULL DEFAULT 'NOT_SENT'
    CHECK (send_status IN ('NOT_SENT', 'SENDING', 'SENT', 'FAILED')),
  ADD COLUMN sent_to VARCHAR(320) NOT NULL DEFAULT '',
  ADD COLUMN sent_at TIMESTAMPTZ,
  ADD COLUMN sent_by_id BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN sent_by_name VARCHAR(100) NOT NULL DEFAULT '',
  ADD COLUMN send_error TEXT NOT NULL DEFAULT '',
  ADD COLUMN send_template_version VARCHAR(32) NOT NULL DEFAULT '',
  ADD COLUMN send_attachment_names JSONB NOT NULL DEFAULT '[]'::jsonb;

CREATE TABLE purchase_order_send_attempts (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  po_id BIGINT NOT NULL REFERENCES purchase_orders(id),
  recipient_email VARCHAR(320) NOT NULL,
  sender_employee_id BIGINT NOT NULL,
  sender_name VARCHAR(100) NOT NULL DEFAULT '',
  status VARCHAR(24) NOT NULL CHECK (status IN ('SENDING', 'SENT', 'FAILED')),
  campaign_id BIGINT NOT NULL DEFAULT 0,
  campaign_no VARCHAR(80) NOT NULL DEFAULT '',
  attachment_names JSONB NOT NULL DEFAULT '[]'::jsonb,
  template_version VARCHAR(32) NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  completed_at TIMESTAMPTZ
);
CREATE INDEX purchase_order_send_attempts_po_idx ON purchase_order_send_attempts (tenant_id, po_id, started_at DESC);

CREATE TABLE purchase_supplier_confirmations (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  po_id BIGINT NOT NULL REFERENCES purchase_orders(id),
  status VARCHAR(40) NOT NULL CHECK (status IN ('MATCHED', 'PENDING_APPROVAL', 'APPROVED', 'REJECTED')),
  confirmed_date DATE NOT NULL,
  confirmed_expected_date DATE,
  remark TEXT NOT NULL DEFAULT '',
  approval_instance_id BIGINT,
  created_by_id BIGINT NOT NULL,
  created_by_name VARCHAR(100) NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX purchase_supplier_confirmations_po_idx ON purchase_supplier_confirmations (tenant_id, po_id, created_at DESC);

CREATE TABLE purchase_supplier_confirmation_lines (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  confirmation_id BIGINT NOT NULL REFERENCES purchase_supplier_confirmations(id) ON DELETE CASCADE,
  po_item_id BIGINT NOT NULL REFERENCES purchase_order_items(id),
  original_qty NUMERIC(18,4) NOT NULL,
  original_unit_price NUMERIC(18,4) NOT NULL,
  confirmed_qty NUMERIC(18,4) NOT NULL CHECK (confirmed_qty > 0),
  confirmed_unit_price NUMERIC(18,4) NOT NULL CHECK (confirmed_unit_price >= 0),
  UNIQUE (confirmation_id, po_item_id)
);

CREATE TABLE purchase_production_milestones (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  po_id BIGINT NOT NULL REFERENCES purchase_orders(id),
  node VARCHAR(32) NOT NULL CHECK (node IN ('PENDING_SCHEDULE', 'SCHEDULED', 'IN_PRODUCTION', 'QUALITY_INSPECTION', 'READY_TO_SHIP', 'SENT_TO_PORT')),
  planned_date DATE,
  actual_date DATE,
  owner_id BIGINT NOT NULL DEFAULT 0,
  owner_name VARCHAR(100) NOT NULL DEFAULT '',
  remark TEXT NOT NULL DEFAULT '',
  attachments JSONB NOT NULL DEFAULT '[]'::jsonb,
  updated_by_id BIGINT NOT NULL,
  updated_by_name VARCHAR(100) NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, po_id, node)
);

CREATE TABLE purchase_production_reminders (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  po_id BIGINT NOT NULL REFERENCES purchase_orders(id),
  milestone_id BIGINT NOT NULL REFERENCES purchase_production_milestones(id) ON DELETE CASCADE,
  node VARCHAR(32) NOT NULL,
  planned_date DATE NOT NULL,
  buyer_id BIGINT NOT NULL DEFAULT 0,
  buyer_name VARCHAR(100) NOT NULL DEFAULT '',
  related_contracts TEXT NOT NULL DEFAULT '',
  status VARCHAR(16) NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN', 'CLOSED')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  closed_at TIMESTAMPTZ,
  UNIQUE (tenant_id, milestone_id)
);

CREATE TABLE purchase_receipt_exceptions (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  po_id BIGINT NOT NULL REFERENCES purchase_orders(id),
  receipt_id BIGINT REFERENCES purchase_receipts(id),
  po_item_id BIGINT REFERENCES purchase_order_items(id),
  exception_type VARCHAR(32) NOT NULL CHECK (exception_type IN ('WRONG_PRODUCT', 'UNIT_MISMATCH', 'SHORT_SHIPMENT', 'DAMAGE', 'QUALITY_DISPUTE', 'RETURN')),
  qty NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (qty >= 0),
  actual_product VARCHAR(200) NOT NULL DEFAULT '',
  actual_uom VARCHAR(32) NOT NULL DEFAULT '',
  description TEXT NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN', 'RESOLVED')),
  resolution TEXT NOT NULL DEFAULT '',
  reported_by_id BIGINT NOT NULL,
  reported_by_name VARCHAR(100) NOT NULL DEFAULT '',
  reported_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_by_id BIGINT NOT NULL DEFAULT 0,
  resolved_by_name VARCHAR(100) NOT NULL DEFAULT '',
  resolved_at TIMESTAMPTZ
);
CREATE INDEX purchase_receipt_exceptions_po_idx ON purchase_receipt_exceptions (tenant_id, po_id, status, reported_at DESC);

-- +goose Down
DROP TABLE purchase_receipt_exceptions;
DROP TABLE purchase_production_reminders;
DROP TABLE purchase_production_milestones;
DROP TABLE purchase_supplier_confirmation_lines;
DROP TABLE purchase_supplier_confirmations;
DROP TABLE purchase_order_send_attempts;
ALTER TABLE purchase_orders
  DROP COLUMN send_attachment_names,
  DROP COLUMN send_template_version,
  DROP COLUMN send_error,
  DROP COLUMN sent_by_name,
  DROP COLUMN sent_by_id,
  DROP COLUMN sent_at,
  DROP COLUMN sent_to,
  DROP COLUMN send_status;

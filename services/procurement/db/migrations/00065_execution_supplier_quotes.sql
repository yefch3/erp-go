-- +goose Up
-- A finance-released contract is quoted again before a purchase-order draft
-- exists. Keep every supplier response so the buyer can compare it, select
-- one, and still retain the alternatives and prior rounds for audit.
CREATE TABLE purchase_execution_supplier_quotes (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  requirement_id BIGINT NOT NULL REFERENCES purchase_requirements(id),
  supplier_id BIGINT NOT NULL,
  supplier_code VARCHAR(50) NOT NULL DEFAULT '',
  supplier_name VARCHAR(200) NOT NULL,
  currency VARCHAR(8) NOT NULL,
  unit_price NUMERIC(20,6) NOT NULL CHECK (unit_price > 0),
  expected_date DATE,
  payment_terms TEXT NOT NULL DEFAULT '',
  valid_until DATE,
  remark TEXT NOT NULL DEFAULT '',
  created_by_id BIGINT NOT NULL DEFAULT 0,
  created_by_name VARCHAR(100) NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX purchase_execution_supplier_quotes_requirement_idx
  ON purchase_execution_supplier_quotes (tenant_id, requirement_id, updated_at DESC, id DESC);

-- +goose Down
DROP TABLE purchase_execution_supplier_quotes;

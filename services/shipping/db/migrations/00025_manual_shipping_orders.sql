-- +goose Up
-- Manual orders keep a reference number, not an invented sales contract ID.
CREATE TABLE manual_shipping_orders (
  handoff_id BIGINT PRIMARY KEY REFERENCES contract_shipping_handoffs(id) ON DELETE CASCADE,
  tenant_id BIGINT NOT NULL,
  order_no VARCHAR(80) NOT NULL,
  amount_missing BOOLEAN NOT NULL DEFAULT TRUE,
  created_by BIGINT NOT NULL,
  created_by_name VARCHAR(100) NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, order_no)
);

-- +goose Down
DROP TABLE manual_shipping_orders;

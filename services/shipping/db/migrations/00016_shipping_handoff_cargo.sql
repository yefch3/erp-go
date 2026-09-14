-- +goose Up
-- Product snapshots needed by Logistics when asking forwarders to re-quote.
-- They deliberately carry no selling price or cost allocation.
CREATE TABLE contract_shipping_handoff_cargo (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  handoff_id BIGINT NOT NULL REFERENCES contract_shipping_handoffs(id) ON DELETE CASCADE,
  contract_item_id BIGINT NOT NULL,
  line_no INT NOT NULL,
  product_code VARCHAR(100) NOT NULL DEFAULT '',
  product_name VARCHAR(200) NOT NULL,
  specification TEXT NOT NULL DEFAULT '',
  quantity NUMERIC(18,4) NOT NULL CHECK (quantity > 0),
  uom_code VARCHAR(20) NOT NULL DEFAULT '',
  remark TEXT NOT NULL DEFAULT '',
  UNIQUE (tenant_id, handoff_id, contract_item_id)
);
CREATE INDEX contract_shipping_handoff_cargo_handoff_idx
  ON contract_shipping_handoff_cargo (tenant_id, handoff_id, line_no, id);

-- +goose Down
DROP TABLE contract_shipping_handoff_cargo;

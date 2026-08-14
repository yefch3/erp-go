-- +goose Up
CREATE SEQUENCE cost_scenario_no_seq;

CREATE TABLE cost_scenarios (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  case_id BIGINT NOT NULL REFERENCES sourcing_cases(id),
  scenario_no VARCHAR(40) NOT NULL,
  currency CHAR(3) NOT NULL,
  allocation_basis VARCHAR(20) NOT NULL CHECK (allocation_basis IN ('TONS','PRODUCT_AMOUNT')),
  margin_type VARCHAR(24) NOT NULL CHECK (margin_type IN ('FIXED_PER_TON','PERCENT')),
  margin_value NUMERIC(18,6) NOT NULL CHECK (margin_value >= 0),
  fx_rate NUMERIC(18,8) NOT NULL,
  fx_rate_at TIMESTAMPTZ NOT NULL,
  fx_source VARCHAR(50) NOT NULL,
  fx_base_currency CHAR(3) NOT NULL DEFAULT 'USD',
  product_total NUMERIC(18,2) NOT NULL,
  charge_total NUMERIC(18,2) NOT NULL,
  landed_total NUMERIC(18,2) NOT NULL,
  margin_total NUMERIC(18,2) NOT NULL,
  customer_total NUMERIC(18,2) NOT NULL,
  status VARCHAR(20) NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT','CONFIRMED','SUPERSEDED')),
  customer_quotation_id BIGINT,
  customer_quote_no VARCHAR(50) NOT NULL DEFAULT '',
  created_by BIGINT NOT NULL,
  created_by_name VARCHAR(120) NOT NULL DEFAULT '',
  confirmed_by BIGINT,
  confirmed_by_name VARCHAR(120) NOT NULL DEFAULT '',
  confirmed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, scenario_no)
);
CREATE INDEX cost_scenarios_case_idx ON cost_scenarios(tenant_id, case_id, created_at DESC);

CREATE TABLE cost_charges (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  scenario_id BIGINT NOT NULL REFERENCES cost_scenarios(id) ON DELETE CASCADE,
  charge_type VARCHAR(32) NOT NULL CHECK (charge_type IN ('ORIGIN_TERMINAL','DESTINATION_TERMINAL','OCEAN_FREIGHT','INSURANCE','DOCUMENT','FINANCE','OTHER')),
  basis VARCHAR(24) NOT NULL CHECK (basis IN ('PER_TON','PER_CONTAINER','PER_SHIPMENT','FIXED')),
  description VARCHAR(200) NOT NULL DEFAULT '',
  origin_port VARCHAR(100) NOT NULL DEFAULT '',
  destination_port VARCHAR(100) NOT NULL DEFAULT '',
  container_type VARCHAR(40) NOT NULL DEFAULT '',
  amount NUMERIC(18,6) NOT NULL CHECK (amount >= 0),
  currency CHAR(3) NOT NULL,
  converted_amount NUMERIC(18,2) NOT NULL,
  source_fx_rate NUMERIC(18,8) NOT NULL,
  target_fx_rate NUMERIC(18,8) NOT NULL,
  fx_rate_at TIMESTAMPTZ NOT NULL,
  fx_source VARCHAR(50) NOT NULL,
  effective_at DATE,
  valid_until DATE,
  source VARCHAR(120) NOT NULL DEFAULT '',
  remark TEXT NOT NULL DEFAULT ''
);

CREATE TABLE cost_scenario_lines (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  scenario_id BIGINT NOT NULL REFERENCES cost_scenarios(id) ON DELETE CASCADE,
  sourcing_line_id BIGINT NOT NULL REFERENCES sourcing_lines(id),
  supplier_quote_line_id BIGINT NOT NULL REFERENCES supplier_quote_lines(id),
  supplier_name VARCHAR(200) NOT NULL,
  product_id BIGINT NOT NULL,
  sku_id BIGINT,
  product_name VARCHAR(200) NOT NULL,
  spec_snapshot TEXT NOT NULL,
  qty NUMERIC(18,4) NOT NULL CHECK (qty > 0),
  uom_code VARCHAR(20) NOT NULL,
  source_currency CHAR(3) NOT NULL,
  source_unit_price NUMERIC(18,6) NOT NULL,
  source_fx_rate NUMERIC(18,8) NOT NULL,
  target_fx_rate NUMERIC(18,8) NOT NULL,
  product_cost NUMERIC(18,2) NOT NULL,
  allocated_charge NUMERIC(18,2) NOT NULL,
  landed_cost NUMERIC(18,6) NOT NULL,
  margin_amount NUMERIC(18,6) NOT NULL,
  customer_unit_price NUMERIC(18,6) NOT NULL,
  customer_amount NUMERIC(18,2) NOT NULL,
  UNIQUE (tenant_id, scenario_id, sourcing_line_id)
);

-- +goose Down
DROP TABLE cost_scenario_lines;
DROP TABLE cost_charges;
DROP TABLE cost_scenarios;
DROP SEQUENCE cost_scenario_no_seq;

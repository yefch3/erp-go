-- +goose Up
CREATE SEQUENCE procurement_plan_no_seq;

CREATE TABLE procurement_plans (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  case_id BIGINT NOT NULL REFERENCES sourcing_cases(id),
  plan_no VARCHAR(40) NOT NULL,
  version_no INT NOT NULL CHECK (version_no > 0),
  requirement_version_no INT NOT NULL CHECK (requirement_version_no > 0),
  status VARCHAR(24) NOT NULL DEFAULT 'CONFIRMED'
    CHECK (status IN ('CONFIRMED','SUBMITTED_TO_SALES','SUPERSEDED')),
  manager_note TEXT NOT NULL DEFAULT '',
  created_by BIGINT NOT NULL,
  created_by_name VARCHAR(120) NOT NULL DEFAULT '',
  confirmed_by BIGINT NOT NULL,
  confirmed_by_name VARCHAR(120) NOT NULL DEFAULT '',
  confirmed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  submitted_to_sales_by BIGINT,
  submitted_to_sales_by_name VARCHAR(120) NOT NULL DEFAULT '',
  submitted_to_sales_at TIMESTAMPTZ,
  target_sales_id BIGINT NOT NULL,
  target_sales_name VARCHAR(120) NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, plan_no),
  UNIQUE (tenant_id, case_id, version_no)
);
CREATE INDEX procurement_plans_case_idx ON procurement_plans(tenant_id, case_id, version_no DESC);

CREATE TABLE procurement_plan_items (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  plan_id BIGINT NOT NULL REFERENCES procurement_plans(id) ON DELETE CASCADE,
  sourcing_line_id BIGINT NOT NULL REFERENCES sourcing_lines(id),
  supplier_quote_line_id BIGINT NOT NULL REFERENCES supplier_quote_lines(id),
  selection_type VARCHAR(16) NOT NULL CHECK (selection_type IN ('RECOMMENDED','BACKUP')),
  priority INT NOT NULL CHECK (priority > 0),
  reason TEXT NOT NULL DEFAULT '',
  risk TEXT NOT NULL DEFAULT '',
  supplier_id BIGINT NOT NULL,
  supplier_name VARCHAR(200) NOT NULL,
  factory_id BIGINT,
  factory_name VARCHAR(200) NOT NULL DEFAULT '',
  buyer_id BIGINT NOT NULL,
  buyer_name VARCHAR(120) NOT NULL DEFAULT '',
  product_name VARCHAR(200) NOT NULL,
  currency CHAR(3) NOT NULL,
  unit_price NUMERIC(18,6) NOT NULL CHECK (unit_price >= 0),
  available_qty NUMERIC(18,4) NOT NULL CHECK (available_qty > 0),
  uom_code VARCHAR(20) NOT NULL,
  moq NUMERIC(18,4),
  lead_time INT,
  payment_terms VARCHAR(200) NOT NULL DEFAULT '',
  incoterm VARCHAR(80) NOT NULL DEFAULT '',
  valid_until DATE,
  quote_version_no INT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, plan_id, supplier_quote_line_id),
  UNIQUE (tenant_id, plan_id, sourcing_line_id, selection_type, priority)
);
CREATE INDEX procurement_plan_items_line_idx ON procurement_plan_items(tenant_id, plan_id, sourcing_line_id, priority);

CREATE TABLE procurement_rework_requests (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  case_id BIGINT NOT NULL REFERENCES sourcing_cases(id),
  plan_id BIGINT REFERENCES procurement_plans(id),
  sourcing_line_id BIGINT REFERENCES sourcing_lines(id),
  supplier_quote_line_id BIGINT REFERENCES supplier_quote_lines(id),
  request_type VARCHAR(24) NOT NULL CHECK (request_type IN ('REQUOTE','RENEGOTIATE','ADD_SUPPLIER')),
  scope_type VARCHAR(16) NOT NULL CHECK (scope_type IN ('QUOTE','PRODUCT','BUYER','ALL')),
  assigned_buyer_id BIGINT,
  assigned_buyer_name VARCHAR(120) NOT NULL DEFAULT '',
  supplier_id BIGINT,
  supplier_name VARCHAR(200) NOT NULL DEFAULT '',
  product_name VARCHAR(200) NOT NULL DEFAULT '',
  reason TEXT NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN','RESOLVED','CANCELLED')),
  created_by BIGINT NOT NULL,
  created_by_name VARCHAR(120) NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_by BIGINT,
  resolved_by_name VARCHAR(120) NOT NULL DEFAULT '',
  resolved_at TIMESTAMPTZ,
  resolution_note TEXT NOT NULL DEFAULT ''
);
CREATE INDEX procurement_rework_case_idx ON procurement_rework_requests(tenant_id, case_id, status, created_at DESC);

ALTER TABLE sourcing_cases DROP CONSTRAINT sourcing_cases_handoff_status_check;
ALTER TABLE sourcing_cases ADD CONSTRAINT sourcing_cases_handoff_status_check CHECK (
  handoff_status IN ('DRAFT','WAITING_ACCEPTANCE','IN_PROGRESS','RETURNED_FOR_SUPPLEMENT',
    'PROCUREMENT_PLAN_READY','PROCUREMENT_PLAN_SUBMITTED','COST_CONFIRMED','SUBMITTED_TO_SALES',
    'QUOTE_IN_PROGRESS','CANCELLED')
);

-- +goose Down
ALTER TABLE sourcing_cases DROP CONSTRAINT sourcing_cases_handoff_status_check;
ALTER TABLE sourcing_cases ADD CONSTRAINT sourcing_cases_handoff_status_check CHECK (
  handoff_status IN ('DRAFT','WAITING_ACCEPTANCE','IN_PROGRESS','RETURNED_FOR_SUPPLEMENT',
    'COST_CONFIRMED','SUBMITTED_TO_SALES','QUOTE_IN_PROGRESS','CANCELLED')
);
DROP TABLE procurement_rework_requests;
DROP TABLE procurement_plan_items;
DROP TABLE procurement_plans;
DROP SEQUENCE procurement_plan_no_seq;

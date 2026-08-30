-- +goose Up
ALTER TABLE sourcing_shipping_requests DROP CONSTRAINT sourcing_shipping_requests_status_check;
UPDATE sourcing_shipping_requests SET status=CASE status
  WHEN 'REQUESTED' THEN 'WAITING_PARTICIPATION'
  WHEN 'IN_PROGRESS' THEN 'QUOTING'
  WHEN 'INDICATIVE_LINKED' THEN 'MANAGER_REVIEW'
  WHEN 'UPDATE_REQUIRED' THEN 'REQUOTE_REQUIRED'
  ELSE status END;
ALTER TABLE sourcing_shipping_requests ADD CONSTRAINT sourcing_shipping_requests_status_check CHECK (
  status IN ('WAITING_PARTICIPATION','QUOTING','MANAGER_REVIEW','PLAN_READY',
    'PLAN_SUBMITTED_TO_SALES','REQUOTE_REQUIRED','CANCELLED')
);
ALTER TABLE sourcing_shipping_requests ALTER COLUMN status SET DEFAULT 'WAITING_PARTICIPATION';

CREATE TABLE sourcing_shipping_participants (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  request_id BIGINT NOT NULL REFERENCES sourcing_shipping_requests(id) ON DELETE CASCADE,
  employee_id BIGINT NOT NULL,
  employee_name VARCHAR(120) NOT NULL DEFAULT '',
  participant_role VARCHAR(16) NOT NULL DEFAULT 'COLLABORATOR'
    CHECK (participant_role IN ('PRIMARY','COLLABORATOR')),
  status VARCHAR(16) NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
  primary_requested_at TIMESTAMPTZ,
  joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id,request_id,employee_id)
);
CREATE UNIQUE INDEX sourcing_shipping_participants_one_primary_idx
  ON sourcing_shipping_participants(tenant_id,request_id)
  WHERE participant_role='PRIMARY' AND status='ACTIVE';
CREATE INDEX sourcing_shipping_participants_request_idx
  ON sourcing_shipping_participants(tenant_id,request_id,joined_at,id);

ALTER TABLE sourcing_shipping_options
  ADD COLUMN version_no INT NOT NULL DEFAULT 1 CHECK (version_no>0),
  ADD COLUMN service_option_name VARCHAR(120) NOT NULL DEFAULT '',
  ADD COLUMN submitted_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE sourcing_shipping_options DROP CONSTRAINT sourcing_shipping_options_status_check;
UPDATE sourcing_shipping_options SET status='SUBMITTED';
ALTER TABLE sourcing_shipping_options ADD CONSTRAINT sourcing_shipping_options_status_check
  CHECK (status IN ('SUBMITTED','RETURNED','CANCELLED'));
CREATE UNIQUE INDEX sourcing_shipping_options_owner_version_idx
  ON sourcing_shipping_options(tenant_id,request_id,created_by,lower(carrier_forwarder),lower(service_option_name),version_no);

CREATE SEQUENCE sourcing_shipping_plan_no_seq;
CREATE TABLE sourcing_shipping_plans (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  request_id BIGINT NOT NULL REFERENCES sourcing_shipping_requests(id),
  plan_no VARCHAR(40) NOT NULL,
  version_no INT NOT NULL CHECK (version_no>0),
  requirement_version_no INT NOT NULL CHECK (requirement_version_no>0),
  status VARCHAR(24) NOT NULL DEFAULT 'CONFIRMED'
    CHECK (status IN ('CONFIRMED','SUBMITTED_TO_SALES','SUPERSEDED')),
  manager_note TEXT NOT NULL DEFAULT '',
  created_by BIGINT NOT NULL,
  created_by_name VARCHAR(120) NOT NULL DEFAULT '',
  confirmed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  submitted_to_sales_by BIGINT,
  submitted_to_sales_by_name VARCHAR(120) NOT NULL DEFAULT '',
  submitted_to_sales_at TIMESTAMPTZ,
  target_sales_id BIGINT NOT NULL,
  target_sales_name VARCHAR(120) NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id,plan_no),
  UNIQUE (tenant_id,request_id,version_no)
);
CREATE INDEX sourcing_shipping_plans_request_idx
  ON sourcing_shipping_plans(tenant_id,request_id,version_no DESC);

CREATE TABLE sourcing_shipping_plan_items (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  plan_id BIGINT NOT NULL REFERENCES sourcing_shipping_plans(id) ON DELETE CASCADE,
  sourcing_line_id BIGINT NOT NULL REFERENCES sourcing_lines(id),
  shipping_option_line_id BIGINT NOT NULL REFERENCES sourcing_shipping_option_lines(id),
  selection_type VARCHAR(16) NOT NULL CHECK (selection_type IN ('RECOMMENDED','BACKUP')),
  priority INT NOT NULL CHECK (priority>0),
  reason TEXT NOT NULL DEFAULT '',
  risk TEXT NOT NULL DEFAULT '',
  carrier_forwarder VARCHAR(200) NOT NULL,
  service_option_name VARCHAR(120) NOT NULL DEFAULT '',
  shipping_employee_id BIGINT NOT NULL,
  shipping_employee_name VARCHAR(120) NOT NULL DEFAULT '',
  product_name VARCHAR(200) NOT NULL,
  currency CHAR(3) NOT NULL,
  charge_basis VARCHAR(24) NOT NULL,
  unit_rate NUMERIC(18,6) NOT NULL,
  total_freight NUMERIC(18,6) NOT NULL,
  port_of_loading VARCHAR(200) NOT NULL DEFAULT '',
  port_of_discharge VARCHAR(200) NOT NULL DEFAULT '',
  estimated_departure DATE,
  estimated_arrival DATE,
  valid_until DATE,
  quote_version_no INT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id,plan_id,shipping_option_line_id),
  UNIQUE (tenant_id,plan_id,sourcing_line_id,selection_type,priority)
);
CREATE INDEX sourcing_shipping_plan_items_line_idx
  ON sourcing_shipping_plan_items(tenant_id,plan_id,sourcing_line_id,priority);

-- +goose Down
DROP TABLE sourcing_shipping_plan_items;
DROP TABLE sourcing_shipping_plans;
DROP SEQUENCE sourcing_shipping_plan_no_seq;
DROP TABLE sourcing_shipping_participants;
DROP INDEX sourcing_shipping_options_owner_version_idx;
ALTER TABLE sourcing_shipping_options DROP COLUMN submitted_at, DROP COLUMN service_option_name, DROP COLUMN version_no;
ALTER TABLE sourcing_shipping_options DROP CONSTRAINT sourcing_shipping_options_status_check;
UPDATE sourcing_shipping_options SET status='INDICATIVE' WHERE status<>'CANCELLED';
ALTER TABLE sourcing_shipping_options ADD CONSTRAINT sourcing_shipping_options_status_check
  CHECK (status IN ('INDICATIVE','CANCELLED'));
ALTER TABLE sourcing_shipping_requests DROP CONSTRAINT sourcing_shipping_requests_status_check;
UPDATE sourcing_shipping_requests SET status=CASE status
  WHEN 'WAITING_PARTICIPATION' THEN 'REQUESTED'
  WHEN 'QUOTING' THEN 'IN_PROGRESS'
  WHEN 'MANAGER_REVIEW' THEN 'INDICATIVE_LINKED'
  WHEN 'PLAN_READY' THEN 'INDICATIVE_LINKED'
  WHEN 'PLAN_SUBMITTED_TO_SALES' THEN 'INDICATIVE_LINKED'
  WHEN 'REQUOTE_REQUIRED' THEN 'UPDATE_REQUIRED'
  ELSE status END;
ALTER TABLE sourcing_shipping_requests ADD CONSTRAINT sourcing_shipping_requests_status_check CHECK (
  status IN ('REQUESTED','IN_PROGRESS','INDICATIVE_LINKED','UPDATE_REQUIRED','CANCELLED')
);
ALTER TABLE sourcing_shipping_requests ALTER COLUMN status SET DEFAULT 'REQUESTED';

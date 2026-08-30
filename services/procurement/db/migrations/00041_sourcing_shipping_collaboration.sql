-- +goose Up
CREATE TABLE sourcing_shipping_requests (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  case_id BIGINT NOT NULL REFERENCES sourcing_cases(id),
  requirement_version_no INT NOT NULL DEFAULT 1,
  status VARCHAR(24) NOT NULL DEFAULT 'REQUESTED'
    CHECK (status IN ('REQUESTED','IN_PROGRESS','INDICATIVE_LINKED','UPDATE_REQUIRED','CANCELLED')),
  case_no VARCHAR(50) NOT NULL,
  case_title VARCHAR(200) NOT NULL DEFAULT '',
  customer_id BIGINT NOT NULL DEFAULT 0,
  customer_name VARCHAR(200) NOT NULL DEFAULT '',
  sales_employee_id BIGINT NOT NULL,
  sales_employee_name VARCHAR(120) NOT NULL DEFAULT '',
  destination_port VARCHAR(200) NOT NULL DEFAULT '',
  cargo_summary TEXT NOT NULL DEFAULT '',
  requested_by BIGINT NOT NULL,
  requested_by_name VARCHAR(120) NOT NULL DEFAULT '',
  requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, case_id)
);
CREATE INDEX sourcing_shipping_requests_status_idx
  ON sourcing_shipping_requests(tenant_id,status,updated_at DESC);

CREATE TABLE sourcing_shipping_options (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  request_id BIGINT NOT NULL REFERENCES sourcing_shipping_requests(id),
  carrier_forwarder VARCHAR(200) NOT NULL,
  port_of_loading VARCHAR(200) NOT NULL DEFAULT '',
  port_of_discharge VARCHAR(200) NOT NULL DEFAULT '',
  quoted_at DATE,
  estimated_departure DATE,
  estimated_arrival DATE,
  valid_until DATE,
  note TEXT NOT NULL DEFAULT '',
  status VARCHAR(16) NOT NULL DEFAULT 'INDICATIVE' CHECK (status IN ('INDICATIVE','CANCELLED')),
  created_by BIGINT NOT NULL,
  created_by_name VARCHAR(120) NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sourcing_shipping_options_request_idx
  ON sourcing_shipping_options(tenant_id,request_id,status,created_at DESC);

CREATE TABLE sourcing_shipping_option_lines (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  option_id BIGINT NOT NULL REFERENCES sourcing_shipping_options(id) ON DELETE CASCADE,
  sourcing_line_id BIGINT NOT NULL REFERENCES sourcing_lines(id),
  line_no INT NOT NULL,
  product_snapshot VARCHAR(200) NOT NULL DEFAULT '',
  specification_snapshot TEXT NOT NULL DEFAULT '',
  quantity NUMERIC(18,4),
  quantity_unit VARCHAR(50) NOT NULL DEFAULT '',
  currency CHAR(3) NOT NULL,
  charge_basis VARCHAR(24) NOT NULL CHECK (charge_basis IN ('PER_TON','PER_CONTAINER','PER_PIECE','PER_SHIPMENT','FIXED')),
  unit_rate NUMERIC(18,6) NOT NULL CHECK (unit_rate >= 0),
  total_freight NUMERIC(18,6) NOT NULL CHECK (total_freight >= 0),
  note TEXT NOT NULL DEFAULT '',
  UNIQUE (tenant_id,option_id,sourcing_line_id)
);
CREATE INDEX sourcing_shipping_option_lines_option_idx
  ON sourcing_shipping_option_lines(tenant_id,option_id,line_no);

-- Existing active sourcing cases also need a shipping work item after rollout.
INSERT INTO sourcing_shipping_requests(
 tenant_id,case_id,requirement_version_no,status,case_no,case_title,customer_id,customer_name,
 sales_employee_id,sales_employee_name,destination_port,cargo_summary,requested_by,requested_by_name,requested_at)
SELECT c.tenant_id,c.id,greatest(c.requirement_version_no,1),'REQUESTED',c.case_no,c.title,c.customer_id,c.customer_name,
 c.owner_id,c.owner_name,coalesce(max(nullif(l.port,'')),''),
 coalesce(string_agg(l.product
   || CASE WHEN l.quantity IS NULL THEN '' ELSE ' ' || l.quantity::text || ' ' || l.quantity_unit END
   || CASE WHEN trim(l.packaging)='' THEN '' ELSE '，包装 ' || l.packaging END
   || CASE WHEN trim(l.delivery)='' THEN '' ELSE '，时间要求 ' || l.delivery END,
   '；' ORDER BY l.line_no) FILTER (WHERE l.decision='CONFIRMED'),''),
 c.owner_id,c.owner_name,coalesce(c.updated_at,c.created_at)
FROM sourcing_cases c
LEFT JOIN sourcing_lines l ON l.tenant_id=c.tenant_id AND l.case_id=c.id
WHERE c.status NOT IN ('INTAKE_PENDING','CANCELLED')
GROUP BY c.tenant_id,c.id
ON CONFLICT (tenant_id,case_id) DO NOTHING;

-- +goose Down
DROP TABLE sourcing_shipping_option_lines;
DROP TABLE sourcing_shipping_options;
DROP TABLE sourcing_shipping_requests;

-- +goose Up
CREATE SEQUENCE sourcing_sales_plan_no_seq;

-- 销售交给客户的每一版组合方案都是快照。采购和船运经理的原始方案只引用，
-- 销售只能填写对客价格、交期和客户可见说明，不能改写内部底价或运费。
CREATE TABLE sourcing_sales_plans (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  case_id BIGINT NOT NULL REFERENCES sourcing_cases(id),
  plan_no VARCHAR(40) NOT NULL,
  version_no INT NOT NULL CHECK (version_no > 0),
  requirement_version_no INT NOT NULL CHECK (requirement_version_no > 0),
  procurement_plan_id BIGINT NOT NULL REFERENCES procurement_plans(id),
  shipping_plan_id BIGINT REFERENCES sourcing_shipping_plans(id),
  status VARCHAR(24) NOT NULL DEFAULT 'PRESENTED'
    CHECK (status IN ('PRESENTED','REWORK_REQUESTED','SUPERSEDED')),
  valid_until DATE NOT NULL,
  customer_note TEXT NOT NULL DEFAULT '',
  internal_note TEXT NOT NULL DEFAULT '',
  created_by BIGINT NOT NULL,
  created_by_name VARCHAR(120) NOT NULL DEFAULT '',
  presented_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, plan_no),
  UNIQUE (tenant_id, case_id, version_no)
);
CREATE INDEX sourcing_sales_plans_case_idx
  ON sourcing_sales_plans(tenant_id, case_id, version_no DESC);

CREATE TABLE sourcing_sales_plan_items (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  plan_id BIGINT NOT NULL REFERENCES sourcing_sales_plans(id) ON DELETE CASCADE,
  sourcing_line_id BIGINT NOT NULL REFERENCES sourcing_lines(id),
  procurement_plan_item_id BIGINT NOT NULL REFERENCES procurement_plan_items(id),
  shipping_plan_item_id BIGINT REFERENCES sourcing_shipping_plan_items(id),
  option_type VARCHAR(20) NOT NULL CHECK (option_type IN ('PRIMARY','ALTERNATIVE')),
  priority INT NOT NULL CHECK (priority > 0),
  product_name VARCHAR(200) NOT NULL,
  quoted_qty NUMERIC(18,4) NOT NULL CHECK (quoted_qty > 0),
  uom_code VARCHAR(20) NOT NULL,
  customer_currency CHAR(3) NOT NULL,
  customer_unit_price NUMERIC(18,6) NOT NULL CHECK (customer_unit_price >= 0),
  promised_delivery_date DATE,
  line_note TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, plan_id, procurement_plan_item_id, shipping_plan_item_id),
  UNIQUE (tenant_id, plan_id, sourcing_line_id, option_type, priority)
);
CREATE INDEX sourcing_sales_plan_items_line_idx
  ON sourcing_sales_plan_items(tenant_id, plan_id, sourcing_line_id, option_type, priority);

-- 人工电话、邮件和线下沟通不记录全过程，只保留推动下一步所需的结果摘要。
CREATE TABLE sourcing_customer_feedback (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  case_id BIGINT NOT NULL REFERENCES sourcing_cases(id),
  sales_plan_id BIGINT NOT NULL REFERENCES sourcing_sales_plans(id),
  contact_name VARCHAR(120) NOT NULL DEFAULT '',
  channel VARCHAR(16) NOT NULL CHECK (channel IN ('PHONE','EMAIL','MEETING','OTHER')),
  result VARCHAR(24) NOT NULL
    CHECK (result IN ('CONTINUE_NEGOTIATION','REQUOTE_REQUIRED','COMMENT')),
  summary TEXT NOT NULL,
  contacted_at TIMESTAMPTZ NOT NULL,
  created_by BIGINT NOT NULL,
  created_by_name VARCHAR(120) NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sourcing_customer_feedback_case_idx
  ON sourcing_customer_feedback(tenant_id, case_id, contacted_at DESC, id DESC);

CREATE TABLE sourcing_shipping_rework_requests (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  case_id BIGINT NOT NULL REFERENCES sourcing_cases(id),
  sales_plan_id BIGINT REFERENCES sourcing_sales_plans(id),
  sourcing_line_id BIGINT REFERENCES sourcing_lines(id),
  shipping_option_line_id BIGINT REFERENCES sourcing_shipping_option_lines(id),
  request_type VARCHAR(24) NOT NULL
    CHECK (request_type IN ('REQUOTE','RENEGOTIATE','ADD_CARRIER')),
  scope_type VARCHAR(16) NOT NULL CHECK (scope_type IN ('QUOTE','PRODUCT','ALL')),
  assigned_shipping_id BIGINT,
  assigned_shipping_name VARCHAR(120) NOT NULL DEFAULT '',
  carrier_forwarder VARCHAR(200) NOT NULL DEFAULT '',
  product_name VARCHAR(200) NOT NULL DEFAULT '',
  reason TEXT NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'OPEN'
    CHECK (status IN ('OPEN','RESOLVED','CANCELLED')),
  created_by BIGINT NOT NULL,
  created_by_name VARCHAR(120) NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_by BIGINT,
  resolved_by_name VARCHAR(120) NOT NULL DEFAULT '',
  resolved_at TIMESTAMPTZ,
  resolution_note TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sourcing_shipping_rework_case_idx
  ON sourcing_shipping_rework_requests(tenant_id, case_id, status, created_at DESC);

-- +goose Down
DROP TABLE sourcing_shipping_rework_requests;
DROP TABLE sourcing_customer_feedback;
DROP TABLE sourcing_sales_plan_items;
DROP TABLE sourcing_sales_plans;
DROP SEQUENCE sourcing_sales_plan_no_seq;

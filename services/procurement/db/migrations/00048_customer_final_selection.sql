-- +goose Up
CREATE SEQUENCE sourcing_customer_selection_no_seq;

-- 客户最终选择是销售沟通方案的不可变快照。后续需求或方案变化只会使它失效，
-- 不覆盖客户当时选了什么，也不删除已经完成的最终复询结果。
CREATE TABLE sourcing_customer_selections (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  case_id BIGINT NOT NULL REFERENCES sourcing_cases(id),
  sales_plan_id BIGINT NOT NULL REFERENCES sourcing_sales_plans(id),
  selection_no VARCHAR(40) NOT NULL,
  version_no INT NOT NULL CHECK (version_no > 0),
  requirement_version_no INT NOT NULL CHECK (requirement_version_no > 0),
  status VARCHAR(28) NOT NULL DEFAULT 'FINAL_RECHECK_PENDING'
    CHECK (status IN ('FINAL_RECHECK_PENDING','FINAL_RECHECKED','INVALIDATED')),
  customer_contact VARCHAR(120) NOT NULL DEFAULT '',
  confirmation_note TEXT NOT NULL DEFAULT '',
  customer_confirmed_at TIMESTAMPTZ NOT NULL,
  created_by BIGINT NOT NULL,
  created_by_name VARCHAR(120) NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  final_rechecked_at TIMESTAMPTZ,
  invalidated_at TIMESTAMPTZ,
  invalidated_reason TEXT NOT NULL DEFAULT '',
  UNIQUE (tenant_id, selection_no),
  UNIQUE (tenant_id, case_id, version_no)
);
CREATE INDEX sourcing_customer_selections_case_idx
  ON sourcing_customer_selections(tenant_id, case_id, version_no DESC);

CREATE TABLE sourcing_customer_selection_items (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  selection_id BIGINT NOT NULL REFERENCES sourcing_customer_selections(id) ON DELETE CASCADE,
  sales_plan_item_id BIGINT NOT NULL REFERENCES sourcing_sales_plan_items(id),
  sourcing_line_id BIGINT NOT NULL REFERENCES sourcing_lines(id),
  procurement_plan_item_id BIGINT NOT NULL REFERENCES procurement_plan_items(id),
  supplier_quote_line_id BIGINT NOT NULL REFERENCES supplier_quote_lines(id),
  shipping_plan_item_id BIGINT REFERENCES sourcing_shipping_plan_items(id),
  shipping_option_line_id BIGINT REFERENCES sourcing_shipping_option_lines(id),
  product_name VARCHAR(200) NOT NULL,
  confirmed_qty NUMERIC(18,4) NOT NULL CHECK (confirmed_qty > 0),
  uom_code VARCHAR(20) NOT NULL,
  customer_currency CHAR(3) NOT NULL,
  customer_unit_price NUMERIC(18,6) NOT NULL CHECK (customer_unit_price >= 0),
  promised_delivery_date DATE,
  line_note TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, selection_id, sourcing_line_id),
  UNIQUE (tenant_id, selection_id, sales_plan_item_id)
);

-- 复询任务继续使用采购/船运已有待办，这张表只负责把任务与冻结快照关联起来，
-- 从而可以可靠判断某一版客户确认是否已经全部复询完成。
CREATE TABLE sourcing_final_recheck_tasks (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  selection_id BIGINT NOT NULL REFERENCES sourcing_customer_selections(id) ON DELETE CASCADE,
  selection_item_id BIGINT NOT NULL REFERENCES sourcing_customer_selection_items(id) ON DELETE CASCADE,
  task_domain VARCHAR(16) NOT NULL CHECK (task_domain IN ('PROCUREMENT','SHIPPING')),
  procurement_rework_id BIGINT REFERENCES procurement_rework_requests(id),
  shipping_rework_id BIGINT REFERENCES sourcing_shipping_rework_requests(id),
  status VARCHAR(16) NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN','RESOLVED','CANCELLED')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_at TIMESTAMPTZ,
  CHECK (
    (task_domain='PROCUREMENT' AND procurement_rework_id IS NOT NULL AND shipping_rework_id IS NULL)
    OR (task_domain='SHIPPING' AND shipping_rework_id IS NOT NULL AND procurement_rework_id IS NULL)
  ),
  UNIQUE (tenant_id, selection_item_id, task_domain)
);
CREATE INDEX sourcing_final_recheck_tasks_selection_idx
  ON sourcing_final_recheck_tasks(tenant_id, selection_id, status);

-- +goose Down
DROP TABLE sourcing_final_recheck_tasks;
DROP TABLE sourcing_customer_selection_items;
DROP TABLE sourcing_customer_selections;
DROP SEQUENCE sourcing_customer_selection_no_seq;

-- +goose Up

-- 销售交给客户的是两类相互独立的候选项：按产品的供应商候选，以及按一票
-- 货物的船运候选。采购/船运经理只审核候选，不再提前替客户把两者绑成一行。
ALTER TABLE procurement_plan_items DROP CONSTRAINT procurement_plan_items_selection_type_check;
ALTER TABLE procurement_plan_items ADD CONSTRAINT procurement_plan_items_selection_type_check
  CHECK (selection_type IN ('RECOMMENDED','BACKUP','REJECTED'));
ALTER TABLE sourcing_shipping_plan_items DROP CONSTRAINT sourcing_shipping_plan_items_selection_type_check;
ALTER TABLE sourcing_shipping_plan_items ADD CONSTRAINT sourcing_shipping_plan_items_selection_type_check
  CHECK (selection_type IN ('RECOMMENDED','BACKUP','REJECTED'));

CREATE TABLE sourcing_sales_shipping_options (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  plan_id BIGINT NOT NULL REFERENCES sourcing_sales_plans(id) ON DELETE CASCADE,
  shipping_option_id BIGINT NOT NULL REFERENCES sourcing_shipping_options(id),
  carrier_forwarder VARCHAR(200) NOT NULL,
  service_option_name VARCHAR(120) NOT NULL DEFAULT '',
  shipping_employee_id BIGINT NOT NULL,
  shipping_employee_name VARCHAR(120) NOT NULL DEFAULT '',
  customer_currency CHAR(3) NOT NULL,
  customer_freight_amount NUMERIC(18,6) NOT NULL CHECK (customer_freight_amount >= 0),
  charge_basis VARCHAR(24) NOT NULL,
  port_of_loading VARCHAR(200) NOT NULL DEFAULT '',
  port_of_discharge VARCHAR(200) NOT NULL DEFAULT '',
  estimated_departure DATE,
  estimated_arrival DATE,
  valid_until DATE,
  customer_note TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, plan_id, shipping_option_id)
);
CREATE INDEX sourcing_sales_shipping_options_plan_idx
  ON sourcing_sales_shipping_options(tenant_id, plan_id, id);

CREATE TABLE sourcing_sales_shipping_option_lines (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  sales_shipping_option_id BIGINT NOT NULL REFERENCES sourcing_sales_shipping_options(id) ON DELETE CASCADE,
  sourcing_line_id BIGINT NOT NULL REFERENCES sourcing_lines(id),
  shipping_plan_item_id BIGINT NOT NULL REFERENCES sourcing_shipping_plan_items(id),
  shipping_option_line_id BIGINT NOT NULL REFERENCES sourcing_shipping_option_lines(id),
  product_name VARCHAR(200) NOT NULL,
  quoted_qty NUMERIC(18,4) NOT NULL CHECK (quoted_qty > 0),
  uom_code VARCHAR(20) NOT NULL,
  UNIQUE (tenant_id, sales_shipping_option_id, sourcing_line_id),
  UNIQUE (tenant_id, sales_shipping_option_id, shipping_plan_item_id)
);

-- 冻结产品时保存客户实际选择的供应商快照以及它所属的默认货运批次。
-- 批次键由服务端按 supplier + factory 生成；不同供应商/工厂绝不会被静默
-- 合并成同一票。
ALTER TABLE sourcing_customer_selection_items
  ADD COLUMN supplier_id BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN supplier_name VARCHAR(200) NOT NULL DEFAULT '',
  ADD COLUMN factory_id BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN factory_name VARCHAR(200) NOT NULL DEFAULT '',
  ADD COLUMN shipment_group_key VARCHAR(100) NOT NULL DEFAULT '';

CREATE TABLE sourcing_customer_selection_shipments (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  selection_id BIGINT NOT NULL REFERENCES sourcing_customer_selections(id) ON DELETE CASCADE,
  shipment_group_key VARCHAR(100) NOT NULL,
  sales_shipping_option_id BIGINT NOT NULL REFERENCES sourcing_sales_shipping_options(id),
  shipping_option_id BIGINT NOT NULL REFERENCES sourcing_shipping_options(id),
  carrier_forwarder VARCHAR(200) NOT NULL,
  service_option_name VARCHAR(120) NOT NULL DEFAULT '',
  shipping_employee_id BIGINT NOT NULL,
  shipping_employee_name VARCHAR(120) NOT NULL DEFAULT '',
  customer_currency CHAR(3) NOT NULL,
  customer_freight_amount NUMERIC(18,6) NOT NULL CHECK (customer_freight_amount >= 0),
  charge_basis VARCHAR(24) NOT NULL,
  port_of_loading VARCHAR(200) NOT NULL DEFAULT '',
  port_of_discharge VARCHAR(200) NOT NULL DEFAULT '',
  estimated_departure DATE,
  estimated_arrival DATE,
  valid_until DATE,
  customer_note TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, selection_id, shipment_group_key)
);

CREATE TABLE sourcing_customer_selection_shipment_items (
  tenant_id BIGINT NOT NULL,
  selection_shipment_id BIGINT NOT NULL REFERENCES sourcing_customer_selection_shipments(id) ON DELETE CASCADE,
  selection_item_id BIGINT NOT NULL REFERENCES sourcing_customer_selection_items(id) ON DELETE CASCADE,
  PRIMARY KEY (tenant_id, selection_shipment_id, selection_item_id),
  UNIQUE (tenant_id, selection_item_id)
);

-- 采购复询仍然逐产品；船运复询改为逐货运批次，只产生一次。
ALTER TABLE sourcing_final_recheck_tasks
  ADD COLUMN selection_shipment_id BIGINT REFERENCES sourcing_customer_selection_shipments(id) ON DELETE CASCADE;
ALTER TABLE sourcing_final_recheck_tasks ALTER COLUMN selection_item_id DROP NOT NULL;
ALTER TABLE sourcing_final_recheck_tasks DROP CONSTRAINT sourcing_final_recheck_tasks_check;
ALTER TABLE sourcing_final_recheck_tasks ADD CONSTRAINT sourcing_final_recheck_tasks_target_check CHECK (
  (task_domain='PROCUREMENT' AND selection_item_id IS NOT NULL AND selection_shipment_id IS NULL
    AND procurement_rework_id IS NOT NULL AND shipping_rework_id IS NULL)
  OR
  (task_domain='SHIPPING' AND procurement_rework_id IS NULL AND shipping_rework_id IS NOT NULL
    AND ((selection_item_id IS NOT NULL AND selection_shipment_id IS NULL)
      OR (selection_item_id IS NULL AND selection_shipment_id IS NOT NULL)))
);
CREATE UNIQUE INDEX sourcing_final_recheck_tasks_shipment_domain_idx
  ON sourcing_final_recheck_tasks(tenant_id, selection_shipment_id, task_domain)
  WHERE selection_shipment_id IS NOT NULL;

-- +goose Down
DELETE FROM procurement_plan_items WHERE selection_type='REJECTED';
DELETE FROM sourcing_shipping_plan_items WHERE selection_type='REJECTED';
ALTER TABLE procurement_plan_items DROP CONSTRAINT procurement_plan_items_selection_type_check;
ALTER TABLE procurement_plan_items ADD CONSTRAINT procurement_plan_items_selection_type_check
  CHECK (selection_type IN ('RECOMMENDED','BACKUP'));
ALTER TABLE sourcing_shipping_plan_items DROP CONSTRAINT sourcing_shipping_plan_items_selection_type_check;
ALTER TABLE sourcing_shipping_plan_items ADD CONSTRAINT sourcing_shipping_plan_items_selection_type_check
  CHECK (selection_type IN ('RECOMMENDED','BACKUP'));
DROP INDEX sourcing_final_recheck_tasks_shipment_domain_idx;
ALTER TABLE sourcing_final_recheck_tasks DROP CONSTRAINT sourcing_final_recheck_tasks_target_check;
ALTER TABLE sourcing_final_recheck_tasks ALTER COLUMN selection_item_id SET NOT NULL;
ALTER TABLE sourcing_final_recheck_tasks DROP COLUMN selection_shipment_id;
ALTER TABLE sourcing_final_recheck_tasks ADD CHECK (
  (task_domain='PROCUREMENT' AND procurement_rework_id IS NOT NULL AND shipping_rework_id IS NULL)
  OR (task_domain='SHIPPING' AND shipping_rework_id IS NOT NULL AND procurement_rework_id IS NULL)
);
DROP TABLE sourcing_customer_selection_shipment_items;
DROP TABLE sourcing_customer_selection_shipments;
ALTER TABLE sourcing_customer_selection_items
  DROP COLUMN shipment_group_key,
  DROP COLUMN factory_name,
  DROP COLUMN factory_id,
  DROP COLUMN supplier_name,
  DROP COLUMN supplier_id;
DROP TABLE sourcing_sales_shipping_option_lines;
DROP TABLE sourcing_sales_shipping_options;

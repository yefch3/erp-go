-- +goose Up
-- 客户先通过销售登记意向；采购/船运提交结构化复询结果后，再由销售登记
-- 客户是否接受。复询完成不再等同于客户最终确认。
ALTER TABLE sourcing_customer_selections DROP CONSTRAINT sourcing_customer_selections_status_check;
ALTER TABLE sourcing_customer_selections ALTER COLUMN status TYPE VARCHAR(40);
UPDATE sourcing_customer_selections
SET status='INVALIDATED',invalidated_at=coalesce(invalidated_at,now()),
    invalidated_reason='流程升级：原最终复询任务缺少结构化结果，请重新登记客户意向'
WHERE status='FINAL_RECHECK_PENDING';
UPDATE sourcing_customer_selections
SET status='INVALIDATED',invalidated_at=coalesce(invalidated_at,now()),
    invalidated_reason='流程升级：原已完成复询缺少结构化结果，请重新登记客户意向'
WHERE status='FINAL_RECHECKED';
ALTER TABLE sourcing_customer_selections ADD CONSTRAINT sourcing_customer_selections_status_check CHECK (
  status IN ('INTENT_RECHECK_PENDING','AWAITING_CUSTOMER_CONFIRMATION','CUSTOMER_CONFIRMED','CUSTOMER_REJECTED','INVALIDATED')
);
ALTER TABLE sourcing_customer_selections
  ADD COLUMN customer_decided_at TIMESTAMPTZ,
  ADD COLUMN customer_decision_note TEXT NOT NULL DEFAULT '';

ALTER TABLE sourcing_customer_selection_items
  ADD COLUMN customer_managed_shipping BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN final_customer_currency CHAR(3),
  ADD COLUMN final_customer_unit_price NUMERIC(18,6) CHECK (final_customer_unit_price >= 0);

ALTER TABLE sourcing_customer_selection_shipments
  ADD COLUMN final_customer_currency CHAR(3),
  ADD COLUMN final_customer_freight_amount NUMERIC(18,6) CHECK (final_customer_freight_amount >= 0);

ALTER TABLE sourcing_final_recheck_tasks
  ADD COLUMN result_note TEXT NOT NULL DEFAULT '',
  ADD COLUMN resolved_by BIGINT,
  ADD COLUMN resolved_by_name VARCHAR(120) NOT NULL DEFAULT '',
  ADD COLUMN final_currency CHAR(3),
  ADD COLUMN final_unit_price NUMERIC(18,6) CHECK (final_unit_price >= 0),
  ADD COLUMN final_available_qty NUMERIC(18,4) CHECK (final_available_qty > 0),
  ADD COLUMN final_lead_time INT CHECK (final_lead_time > 0),
  ADD COLUMN final_delivery_date DATE,
  ADD COLUMN final_payment_terms VARCHAR(120) NOT NULL DEFAULT '',
  ADD COLUMN final_incoterm VARCHAR(40) NOT NULL DEFAULT '',
  ADD COLUMN final_valid_until DATE,
  ADD COLUMN final_freight_amount NUMERIC(18,6) CHECK (final_freight_amount >= 0),
  ADD COLUMN final_estimated_departure DATE,
  ADD COLUMN final_estimated_arrival DATE;

-- +goose Down
ALTER TABLE sourcing_final_recheck_tasks
  DROP COLUMN final_estimated_arrival,
  DROP COLUMN final_estimated_departure,
  DROP COLUMN final_freight_amount,
  DROP COLUMN final_valid_until,
  DROP COLUMN final_incoterm,
  DROP COLUMN final_payment_terms,
  DROP COLUMN final_delivery_date,
  DROP COLUMN final_lead_time,
  DROP COLUMN final_available_qty,
  DROP COLUMN final_unit_price,
  DROP COLUMN final_currency,
  DROP COLUMN resolved_by_name,
  DROP COLUMN resolved_by,
  DROP COLUMN result_note;
ALTER TABLE sourcing_customer_selection_shipments
  DROP COLUMN final_customer_freight_amount,
  DROP COLUMN final_customer_currency;
ALTER TABLE sourcing_customer_selection_items
  DROP COLUMN final_customer_unit_price,
  DROP COLUMN final_customer_currency,
  DROP COLUMN customer_managed_shipping;
ALTER TABLE sourcing_customer_selections
  DROP COLUMN customer_decision_note,
  DROP COLUMN customer_decided_at;
ALTER TABLE sourcing_customer_selections DROP CONSTRAINT sourcing_customer_selections_status_check;
UPDATE sourcing_customer_selections SET status='FINAL_RECHECK_PENDING' WHERE status='INTENT_RECHECK_PENDING' OR invalidated_reason='流程升级：原最终复询任务缺少结构化结果，请重新登记客户意向';
UPDATE sourcing_customer_selections SET status='FINAL_RECHECKED' WHERE status IN ('AWAITING_CUSTOMER_CONFIRMATION','CUSTOMER_CONFIRMED','CUSTOMER_REJECTED') OR invalidated_reason='流程升级：原已完成复询缺少结构化结果，请重新登记客户意向';
ALTER TABLE sourcing_customer_selections ALTER COLUMN status TYPE VARCHAR(28);
ALTER TABLE sourcing_customer_selections ADD CONSTRAINT sourcing_customer_selections_status_check
  CHECK (status IN ('FINAL_RECHECK_PENDING','FINAL_RECHECKED','INVALIDATED'));

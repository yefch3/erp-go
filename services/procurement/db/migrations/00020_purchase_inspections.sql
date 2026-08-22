-- +goose Up

-- 质检记录（A3）。收货—质检—入库的关系定为：收货即入码头库（现状不变，
-- 库存事件在收货时已经发出），质检挂在收货单上事后登记；不合格不改库存，
-- 它开出一条待处置的记录，处置（退货/扣款/让步接收/返工）是人的决定，
-- 记下来供扣款和纠纷取证。与 purchase_receipt_exceptions 平行而不合并：
-- 异常是「到的货不对」，质检是「到的货验过了怎么样」——前者可以没有
-- 质检结论，后者可以一切正常。
CREATE TABLE purchase_inspections (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  po_id BIGINT NOT NULL REFERENCES purchase_orders(id),
  -- 质检必须指向一张收货单：没有到货就没有可检的东西。
  receipt_id BIGINT NOT NULL REFERENCES purchase_receipts(id),
  po_item_id BIGINT REFERENCES purchase_order_items(id),
  result VARCHAR(16) NOT NULL CHECK (result IN ('PASS', 'FAIL')),
  inspected_qty NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (inspected_qty >= 0),
  defect_qty NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (defect_qty >= 0),
  note TEXT NOT NULL DEFAULT '',
  -- 与生产里程碑同一形状：[{fileName,fileUrl,contentType}]。
  attachments JSONB NOT NULL DEFAULT '[]'::jsonb,
  -- PASS 生而 RESOLVED（没有要处置的东西）；FAIL 生而 OPEN，登记处置后转
  -- RESOLVED。未 RESOLVED 的质检阻止采购结案。
  status VARCHAR(16) NOT NULL CHECK (status IN ('OPEN', 'RESOLVED')),
  disposition VARCHAR(32) NOT NULL DEFAULT ''
    CHECK (disposition IN ('', 'RETURN', 'DEDUCTION', 'CONCESSION', 'REWORK')),
  disposition_note TEXT NOT NULL DEFAULT '',
  inspected_by_id BIGINT NOT NULL,
  inspected_by_name VARCHAR(100) NOT NULL DEFAULT '',
  inspected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_by_id BIGINT NOT NULL DEFAULT 0,
  resolved_by_name VARCHAR(100) NOT NULL DEFAULT '',
  resolved_at TIMESTAMPTZ
);
CREATE INDEX purchase_inspections_po_idx
  ON purchase_inspections (tenant_id, po_id, status, inspected_at DESC);

-- 结案是订单生命周期之外的一个事实，不是一个新状态：状态机到 RECEIVED
-- 封顶（收满自动到达），对账、工作台、三单核对按状态过滤的地方一概不用
-- 改。结案的闸门在应用层：全部收满、无未解决异常、无未解决质检。
ALTER TABLE purchase_orders ADD COLUMN closed_at TIMESTAMPTZ;
ALTER TABLE purchase_orders ADD COLUMN closed_by_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE purchase_orders ADD COLUMN closed_by_name VARCHAR(100) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE purchase_orders DROP COLUMN closed_by_name;
ALTER TABLE purchase_orders DROP COLUMN closed_by_id;
ALTER TABLE purchase_orders DROP COLUMN closed_at;
DROP TABLE purchase_inspections;

-- +goose Up

-- P8：客户接受报价后，按已确认成本方案生成可下单明细。
-- 继续复用 purchase_requirements，是为了让既有采购单、审批、收货和对账链路保持一致。
ALTER TABLE purchase_requirements DROP CONSTRAINT purchase_requirements_source_check;
ALTER TABLE purchase_requirements ADD CONSTRAINT purchase_requirements_source_check
    CHECK (source IN ('CONTRACT', 'STOCK_ALERT', 'MANUAL', 'CUSTOMER_QUOTATION'));

ALTER TABLE purchase_requirements
    ADD COLUMN quotation_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN quotation_no VARCHAR(50) NOT NULL DEFAULT '',
    ADD COLUMN cost_scenario_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN cost_scenario_no VARCHAR(50) NOT NULL DEFAULT '',
    ADD COLUMN sourcing_case_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN sourcing_line_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN supplier_quote_line_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN supplier_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN supplier_code VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN supplier_name VARCHAR(200) NOT NULL DEFAULT '',
    ADD COLUMN factory_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN factory_code VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN factory_name VARCHAR(200) NOT NULL DEFAULT '',
    ADD COLUMN source_currency VARCHAR(10) NOT NULL DEFAULT '',
    ADD COLUMN source_unit_price NUMERIC(18,6) NOT NULL DEFAULT 0 CHECK (source_unit_price >= 0),
    ADD COLUMN moq NUMERIC(18,4),
    ADD COLUMN lead_time VARCHAR(120) NOT NULL DEFAULT '';

CREATE UNIQUE INDEX purchase_requirements_quotation_line_idx
    ON purchase_requirements (tenant_id, quotation_id, sourcing_line_id)
    WHERE source = 'CUSTOMER_QUOTATION';
CREATE INDEX purchase_requirements_supplier_ordering_idx
    ON purchase_requirements (tenant_id, source, status, supplier_id, quotation_id);

-- 履约方式属于采购单快照：直发不要求仓库；入库后发货才要求仓库。
ALTER TABLE purchase_orders
    ADD COLUMN source_quotation_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN source_quotation_no VARCHAR(50) NOT NULL DEFAULT '',
    ADD COLUMN source_cost_scenario_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN factory_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN factory_code VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN factory_name VARCHAR(200) NOT NULL DEFAULT '',
    ADD COLUMN fulfillment_mode VARCHAR(24) NOT NULL DEFAULT 'DIRECT_SHIP'
        CHECK (fulfillment_mode IN ('DIRECT_SHIP', 'WAREHOUSE')),
    ADD COLUMN delivery_location_type VARCHAR(24) NOT NULL DEFAULT 'PORT'
        CHECK (delivery_location_type IN ('PORT', 'WAREHOUSE', 'CUSTOM')),
    ADD COLUMN delivery_port_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN delivery_port_code VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN delivery_port_name VARCHAR(200) NOT NULL DEFAULT '',
    ADD COLUMN warehouse_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN warehouse_name VARCHAR(200) NOT NULL DEFAULT '',
    ADD COLUMN delivery_address TEXT NOT NULL DEFAULT '',
    ADD COLUMN source_change_reason TEXT NOT NULL DEFAULT '';

-- 同一份客户报价、同一供应商只能存在一张仍有效的采购单，防止重复点击下单。
CREATE UNIQUE INDEX purchase_orders_quotation_supplier_idx
    ON purchase_orders (tenant_id, source_quotation_id, supplier_id)
    WHERE source_quotation_id > 0 AND status <> 'CANCELLED';

-- +goose Down
DROP INDEX IF EXISTS purchase_orders_quotation_supplier_idx;
ALTER TABLE purchase_orders
    DROP COLUMN source_change_reason,
    DROP COLUMN delivery_address,
    DROP COLUMN warehouse_name,
    DROP COLUMN warehouse_id,
    DROP COLUMN delivery_port_name,
    DROP COLUMN delivery_port_code,
    DROP COLUMN delivery_port_id,
    DROP COLUMN delivery_location_type,
    DROP COLUMN fulfillment_mode,
    DROP COLUMN factory_name,
    DROP COLUMN factory_code,
    DROP COLUMN factory_id,
    DROP COLUMN source_cost_scenario_id,
    DROP COLUMN source_quotation_no,
    DROP COLUMN source_quotation_id;

DROP INDEX IF EXISTS purchase_requirements_supplier_ordering_idx;
DROP INDEX IF EXISTS purchase_requirements_quotation_line_idx;
ALTER TABLE purchase_requirements
    DROP COLUMN lead_time,
    DROP COLUMN moq,
    DROP COLUMN source_unit_price,
    DROP COLUMN source_currency,
    DROP COLUMN factory_name,
    DROP COLUMN factory_code,
    DROP COLUMN factory_id,
    DROP COLUMN supplier_name,
    DROP COLUMN supplier_code,
    DROP COLUMN supplier_id,
    DROP COLUMN supplier_quote_line_id,
    DROP COLUMN sourcing_line_id,
    DROP COLUMN sourcing_case_id,
    DROP COLUMN cost_scenario_no,
    DROP COLUMN cost_scenario_id,
    DROP COLUMN quotation_no,
    DROP COLUMN quotation_id;
ALTER TABLE purchase_requirements DROP CONSTRAINT purchase_requirements_source_check;
ALTER TABLE purchase_requirements ADD CONSTRAINT purchase_requirements_source_check
    CHECK (source IN ('CONTRACT', 'STOCK_ALERT', 'MANUAL'));

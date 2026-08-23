-- +goose Up

-- 属主（A1 收尾）。归属规则由业务拍板：合同拆出的需求归合同负责人
-- （合同是谁谈的，拆出来的采购动向就归谁看），手工需求归创建人。
-- 采购单则归实际执行的采购员——两条线各自独立，订单明细上再把
-- 合同负责人带出来，谁谈的生意不会因为换人下单而失联。
--
-- 0 = 属主未知的历史数据。围栏 fail-closed：这类行只有「全部」范围
-- 能看见，不会漏给任何按人过滤的视角。上线后用
-- scripts/backfill-requirement-owners.sh 从 export 的合同表补齐。
ALTER TABLE purchase_requirements ADD COLUMN owner_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE purchase_requirements ADD COLUMN owner_name VARCHAR(100) NOT NULL DEFAULT '';

CREATE INDEX purchase_requirements_owner_idx
  ON purchase_requirements (tenant_id, owner_id, status);

-- +goose Down
DROP INDEX purchase_requirements_owner_idx;
ALTER TABLE purchase_requirements DROP COLUMN owner_name;
ALTER TABLE purchase_requirements DROP COLUMN owner_id;

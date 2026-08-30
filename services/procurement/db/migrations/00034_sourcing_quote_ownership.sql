-- +goose Up
-- T4: 同一工厂可以由多名采购分别维护报价，但每名采购在同一案件中
-- 仍只有一条有效的工厂询价链；报价版本继续挂在各自的询价链下。
DROP INDEX IF EXISTS factory_rfqs_case_factory_idx;
CREATE UNIQUE INDEX factory_rfqs_case_factory_owner_idx
    ON factory_rfqs (tenant_id, case_id, factory_id, created_by)
    WHERE factory_id > 0 AND status NOT IN ('CANCELLED', 'CLOSED');

-- +goose Down
DROP INDEX IF EXISTS factory_rfqs_case_factory_owner_idx;
CREATE UNIQUE INDEX factory_rfqs_case_factory_idx
    ON factory_rfqs (tenant_id, case_id, factory_id)
    WHERE factory_id > 0 AND status NOT IN ('CANCELLED', 'CLOSED');

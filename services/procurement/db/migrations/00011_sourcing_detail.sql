-- +goose Up

-- P6 保存手工导入的标准询盘原文件。邮件来源仍可通过已有邮件附件标识追溯。
ALTER TABLE sourcing_cases ADD COLUMN source_file_name VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE sourcing_cases ADD COLUMN source_content_type VARCHAR(120) NOT NULL DEFAULT '';
ALTER TABLE sourcing_cases ADD COLUMN source_file_data BYTEA;

-- 已确认明细再次修改时递增，用于清楚区分每次规格修订。
ALTER TABLE sourcing_lines ADD COLUMN revision_no INT NOT NULL DEFAULT 1;

-- 历史 RFQ 允许没有工厂；P6 新建 RFQ 由应用层强制选择具体合作工厂。
ALTER TABLE factory_rfqs ADD COLUMN factory_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE factory_rfqs ADD COLUMN factory_code VARCHAR(100) NOT NULL DEFAULT '';
ALTER TABLE factory_rfqs ADD COLUMN factory_name VARCHAR(200) NOT NULL DEFAULT '';
ALTER TABLE factory_rfqs DROP CONSTRAINT factory_rfqs_tenant_id_case_id_supplier_id_key;
CREATE UNIQUE INDEX factory_rfqs_case_factory_idx
    ON factory_rfqs (tenant_id, case_id, factory_id)
    WHERE factory_id > 0 AND status NOT IN ('CANCELLED', 'CLOSED');

-- 采购项目统一审计轨迹：规格、RFQ、报价及状态变化都写入同一张表。
CREATE TABLE sourcing_case_changes (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL DEFAULT 1,
    case_id BIGINT NOT NULL REFERENCES sourcing_cases(id) ON DELETE CASCADE,
    section VARCHAR(32) NOT NULL,
    action VARCHAR(48) NOT NULL,
    entity_id BIGINT NOT NULL DEFAULT 0,
    summary VARCHAR(500) NOT NULL DEFAULT '',
    before_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    after_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    reason VARCHAR(500) NOT NULL DEFAULT '',
    operator_id BIGINT NOT NULL DEFAULT 0,
    operator_name VARCHAR(150) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sourcing_case_changes_case_idx
    ON sourcing_case_changes (tenant_id, case_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE sourcing_case_changes;
DROP INDEX IF EXISTS factory_rfqs_case_factory_idx;
ALTER TABLE factory_rfqs ADD CONSTRAINT factory_rfqs_tenant_id_case_id_supplier_id_key
    UNIQUE (tenant_id, case_id, supplier_id);
ALTER TABLE factory_rfqs DROP COLUMN factory_name;
ALTER TABLE factory_rfqs DROP COLUMN factory_code;
ALTER TABLE factory_rfqs DROP COLUMN factory_id;
ALTER TABLE sourcing_lines DROP COLUMN revision_no;
ALTER TABLE sourcing_cases DROP COLUMN source_file_data;
ALTER TABLE sourcing_cases DROP COLUMN source_content_type;
ALTER TABLE sourcing_cases DROP COLUMN source_file_name;

-- +goose Up

-- 公司级标准询盘模板。每次修改都新增版本，历史导入继续引用原版本。
CREATE TABLE inquiry_templates (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL DEFAULT 1,
    template_code VARCHAR(60) NOT NULL,
    version INT NOT NULL,
    name VARCHAR(150) NOT NULL,
    description VARCHAR(500) NOT NULL DEFAULT '',
    status VARCHAR(20) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'SUPERSEDED', 'DISABLED')),
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_by BIGINT NOT NULL DEFAULT 0,
    created_by_name VARCHAR(150) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, template_code, version)
);
CREATE UNIQUE INDEX inquiry_templates_active_code_idx
    ON inquiry_templates (tenant_id, template_code) WHERE status = 'ACTIVE';
CREATE UNIQUE INDEX inquiry_templates_default_idx
    ON inquiry_templates (tenant_id) WHERE status = 'ACTIVE' AND is_default;

CREATE TABLE inquiry_template_fields (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL DEFAULT 1,
    template_id BIGINT NOT NULL REFERENCES inquiry_templates(id) ON DELETE CASCADE,
    field_key VARCHAR(80) NOT NULL,
    display_name VARCHAR(150) NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    is_required BOOLEAN NOT NULL DEFAULT FALSE,
    default_value VARCHAR(500) NOT NULL DEFAULT '',
    data_type VARCHAR(20) NOT NULL DEFAULT 'TEXT'
        CHECK (data_type IN ('TEXT', 'NUMBER', 'DATE')),
    is_custom BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE (tenant_id, template_id, field_key),
    UNIQUE (tenant_id, template_id, display_name)
);
CREATE INDEX inquiry_template_fields_order_idx
    ON inquiry_template_fields (tenant_id, template_id, sort_order, id);

ALTER TABLE sourcing_cases ADD COLUMN inquiry_template_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE sourcing_cases ADD COLUMN inquiry_template_code VARCHAR(60) NOT NULL DEFAULT 'SYSTEM_DEFAULT';
ALTER TABLE sourcing_cases ADD COLUMN inquiry_template_version INT NOT NULL DEFAULT 1;
ALTER TABLE sourcing_lines ADD COLUMN custom_fields JSONB NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE sourcing_lines DROP COLUMN custom_fields;
ALTER TABLE sourcing_cases DROP COLUMN inquiry_template_version;
ALTER TABLE sourcing_cases DROP COLUMN inquiry_template_code;
ALTER TABLE sourcing_cases DROP COLUMN inquiry_template_id;
DROP TABLE inquiry_template_fields;
DROP TABLE inquiry_templates;

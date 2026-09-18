-- +goose Up

-- 租户级客户字段定义。field_key 是稳定标识，display_name 可以随业务修改；
-- aliases 保存曾经导入过的 Excel 表头，用于下次自动匹配。
CREATE TABLE customer_field_definitions (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    field_key TEXT NOT NULL,
    display_name TEXT NOT NULL,
    aliases TEXT[] NOT NULL DEFAULT '{}',
    sort_order INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'INACTIVE')),
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, field_key)
);

CREATE INDEX customer_field_definitions_tenant_idx
    ON customer_field_definitions (tenant_id, status, sort_order, id);

-- 值与定义分离，增加新 title 时无需 ALTER customers，也不会影响旧客户。
CREATE TABLE customer_custom_field_values (
    tenant_id BIGINT NOT NULL,
    customer_id BIGINT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    field_id BIGINT NOT NULL REFERENCES customer_field_definitions(id) ON DELETE CASCADE,
    value TEXT NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, customer_id, field_id)
);

CREATE INDEX customer_custom_field_values_customer_idx
    ON customer_custom_field_values (tenant_id, customer_id);

-- +goose Down

DROP TABLE IF EXISTS customer_custom_field_values;
DROP TABLE IF EXISTS customer_field_definitions;

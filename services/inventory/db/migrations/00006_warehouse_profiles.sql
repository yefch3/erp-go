-- +goose Up
-- WH1：仓库启用模式、增强档案和联系人。所有变更均为兼容性扩展，旧仓库与库存引用不变。

CREATE TABLE warehouse_settings (
    tenant_id BIGINT PRIMARY KEY,
    usage_mode TEXT NOT NULL DEFAULT 'USE_WAREHOUSE'
        CHECK (usage_mode IN ('NO_WAREHOUSE', 'USE_WAREHOUSE', 'SELECT_PER_ORDER')),
    allow_direct_delivery BOOLEAN NOT NULL DEFAULT TRUE,
    allow_inventory BOOLEAN NOT NULL DEFAULT TRUE,
    default_warehouse_id BIGINT REFERENCES warehouses(id) ON DELETE SET NULL,
    updated_by BIGINT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE warehouse_settings_history (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    usage_mode TEXT NOT NULL,
    allow_direct_delivery BOOLEAN NOT NULL,
    allow_inventory BOOLEAN NOT NULL,
    default_warehouse_id BIGINT,
    changed_by BIGINT,
    changed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_warehouse_settings_history_tenant
    ON warehouse_settings_history(tenant_id, changed_at DESC);

ALTER TABLE warehouses
    ADD COLUMN profile_type TEXT NOT NULL DEFAULT 'OWN'
        CHECK (profile_type IN ('OWN', 'PORT', 'THIRD_PARTY')),
    ADD COLUMN country_code TEXT NOT NULL DEFAULT '',
    ADD COLUMN city TEXT NOT NULL DEFAULT '',
    ADD COLUMN timezone TEXT NOT NULL DEFAULT 'UTC',
    ADD COLUMN latitude NUMERIC(9,6),
    ADD COLUMN longitude NUMERIC(9,6),
    ADD COLUMN accounting_mode TEXT NOT NULL DEFAULT 'SYNC_INVENTORY'
        CHECK (accounting_mode IN ('DOCUMENT_ONLY', 'SYNC_INVENTORY')),
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

UPDATE warehouses SET profile_type = 'PORT' WHERE wh_type = 'PORT_TERMINAL';

CREATE TABLE warehouse_contacts (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    warehouse_id BIGINT NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
    contact_type TEXT NOT NULL CHECK (contact_type IN ('OWNER', 'CONTACT')),
    employee_id BIGINT,
    name TEXT NOT NULL,
    phone TEXT NOT NULL DEFAULT '',
    email TEXT NOT NULL DEFAULT '',
    is_primary BOOLEAN NOT NULL DEFAULT FALSE,
    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'INACTIVE')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_warehouse_contacts_lookup
    ON warehouse_contacts(tenant_id, warehouse_id, contact_type, status);
CREATE UNIQUE INDEX uq_warehouse_primary_owner
    ON warehouse_contacts(tenant_id, warehouse_id)
    WHERE contact_type = 'OWNER' AND is_primary AND status = 'ACTIVE';
CREATE UNIQUE INDEX uq_warehouse_primary_contact
    ON warehouse_contacts(tenant_id, warehouse_id)
    WHERE contact_type = 'CONTACT' AND is_primary AND status = 'ACTIVE';

CREATE TABLE warehouse_change_history (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    warehouse_id BIGINT NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
    action TEXT NOT NULL,
    before_data JSONB NOT NULL DEFAULT '{}'::jsonb,
    after_data JSONB NOT NULL DEFAULT '{}'::jsonb,
    reason TEXT NOT NULL DEFAULT '',
    changed_by BIGINT,
    changed_by_name TEXT NOT NULL DEFAULT '',
    changed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_warehouse_change_history_lookup
    ON warehouse_change_history(tenant_id, warehouse_id, changed_at DESC);

INSERT INTO warehouse_settings (tenant_id, usage_mode, allow_direct_delivery, allow_inventory, default_warehouse_id)
SELECT tenant_id, 'USE_WAREHOUSE', TRUE, TRUE, min(id)
FROM warehouses
GROUP BY tenant_id
ON CONFLICT (tenant_id) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS warehouse_change_history;
DROP TABLE IF EXISTS warehouse_contacts;
ALTER TABLE warehouses
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS accounting_mode,
    DROP COLUMN IF EXISTS longitude,
    DROP COLUMN IF EXISTS latitude,
    DROP COLUMN IF EXISTS timezone,
    DROP COLUMN IF EXISTS city,
    DROP COLUMN IF EXISTS country_code,
    DROP COLUMN IF EXISTS profile_type;
DROP TABLE IF EXISTS warehouse_settings_history;
DROP TABLE IF EXISTS warehouse_settings;

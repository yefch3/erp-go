-- +goose Up

-- B4：供应商仍是签约/结算主体；工厂是其名下的实际生产地点。
-- 保留 suppliers.name/country/address 等旧字段，确保采购、船期等已有调用不受影响。
ALTER TABLE suppliers
    ADD COLUMN name_zh             VARCHAR(200) NOT NULL DEFAULT '',
    ADD COLUMN name_en             VARCHAR(200) NOT NULL DEFAULT '',
    ADD COLUMN short_name          VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN country_code        CHAR(2)      NOT NULL DEFAULT '',
    ADD COLUMN tax_id              VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN registered_address  VARCHAR(500) NOT NULL DEFAULT '',
    ADD COLUMN payment_term        VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN business_types      TEXT[]       NOT NULL DEFAULT ARRAY['GENERAL']::TEXT[];

UPDATE suppliers
SET name_zh = name,
    country_code = CASE WHEN length(trim(country)) = 2 THEN upper(trim(country)) ELSE '' END,
    registered_address = address
WHERE name_zh = '';

CREATE INDEX suppliers_country_idx ON suppliers (tenant_id, country_code, status);
CREATE INDEX suppliers_business_types_idx ON suppliers USING GIN (business_types);

CREATE TABLE supplier_contacts (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   BIGINT       NOT NULL DEFAULT 1,
    supplier_id BIGINT       NOT NULL REFERENCES suppliers(id) ON DELETE RESTRICT,
    name        VARCHAR(100) NOT NULL,
    department  VARCHAR(100) NOT NULL DEFAULT '',
    title       VARCHAR(100) NOT NULL DEFAULT '',
    phone       VARCHAR(50)  NOT NULL DEFAULT '',
    email       VARCHAR(200) NOT NULL DEFAULT '',
    is_primary  BOOLEAN      NOT NULL DEFAULT false,
    status      VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    remark      TEXT         NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_by  BIGINT       NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_by  BIGINT       NOT NULL DEFAULT 0
);
CREATE INDEX supplier_contacts_supplier_idx ON supplier_contacts (tenant_id, supplier_id, status, id);
CREATE UNIQUE INDEX supplier_contacts_one_primary_idx
    ON supplier_contacts (tenant_id, supplier_id) WHERE is_primary AND status = 'ACTIVE';

CREATE TABLE supplier_owners (
    id                  BIGSERIAL PRIMARY KEY,
    tenant_id           BIGINT       NOT NULL DEFAULT 1,
    supplier_id         BIGINT       NOT NULL REFERENCES suppliers(id) ON DELETE RESTRICT,
    employee_id         BIGINT       NOT NULL,
    employee_name       VARCHAR(100) NOT NULL,
    responsibility_code VARCHAR(50) NOT NULL,
    is_primary          BOOLEAN      NOT NULL DEFAULT false,
    start_date          DATE,
    end_date            DATE,
    status              VARCHAR(32) NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by          BIGINT      NOT NULL DEFAULT 0,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by          BIGINT      NOT NULL DEFAULT 0,
    CHECK (end_date IS NULL OR start_date IS NULL OR end_date >= start_date)
);
CREATE INDEX supplier_owners_supplier_idx ON supplier_owners (tenant_id, supplier_id, status, id);
CREATE INDEX supplier_owners_employee_idx ON supplier_owners (tenant_id, employee_id, status, supplier_id);
CREATE UNIQUE INDEX supplier_owners_active_unique_idx
    ON supplier_owners (tenant_id, supplier_id, employee_id, responsibility_code) WHERE status = 'ACTIVE';
CREATE UNIQUE INDEX supplier_owners_one_primary_idx
    ON supplier_owners (tenant_id, supplier_id) WHERE is_primary AND status = 'ACTIVE';

CREATE TABLE factories (
    id              BIGSERIAL PRIMARY KEY,
    tenant_id       BIGINT       NOT NULL DEFAULT 1,
    supplier_id     BIGINT       NOT NULL REFERENCES suppliers(id) ON DELETE RESTRICT,
    code            VARCHAR(50)  NOT NULL,
    name_zh         VARCHAR(200) NOT NULL DEFAULT '',
    name_en         VARCHAR(200) NOT NULL DEFAULT '',
    short_name      VARCHAR(100) NOT NULL DEFAULT '',
    country_code    CHAR(2)      NOT NULL DEFAULT '',
    timezone        VARCHAR(100) NOT NULL DEFAULT '',
    state_province  VARCHAR(120) NOT NULL DEFAULT '',
    city            VARCHAR(120) NOT NULL DEFAULT '',
    district        VARCHAR(120) NOT NULL DEFAULT '',
    postal_code     VARCHAR(30)  NOT NULL DEFAULT '',
    address         VARCHAR(500) NOT NULL DEFAULT '',
    status          VARCHAR(32)  NOT NULL DEFAULT 'PREPARING'
                    CHECK (status IN ('PREPARING','COOPERATING','SUSPENDED','INACTIVE')),
    remark          TEXT         NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_by      BIGINT       NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_by      BIGINT       NOT NULL DEFAULT 0,
    UNIQUE (tenant_id, code),
    CHECK (btrim(name_zh) <> '' OR btrim(name_en) <> ''),
    CHECK (status <> 'COOPERATING' OR (country_code <> '' AND timezone <> ''))
);
CREATE INDEX factories_supplier_idx ON factories (tenant_id, supplier_id, status, id);
CREATE INDEX factories_country_idx ON factories (tenant_id, country_code, status, city);

CREATE TABLE factory_contacts (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   BIGINT       NOT NULL DEFAULT 1,
    factory_id  BIGINT       NOT NULL REFERENCES factories(id) ON DELETE RESTRICT,
    name        VARCHAR(100) NOT NULL,
    department  VARCHAR(100) NOT NULL DEFAULT '',
    title       VARCHAR(100) NOT NULL DEFAULT '',
    phone       VARCHAR(50)  NOT NULL DEFAULT '',
    email       VARCHAR(200) NOT NULL DEFAULT '',
    is_primary  BOOLEAN      NOT NULL DEFAULT false,
    status      VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    remark      TEXT         NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_by  BIGINT       NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_by  BIGINT       NOT NULL DEFAULT 0
);
CREATE INDEX factory_contacts_factory_idx ON factory_contacts (tenant_id, factory_id, status, id);
CREATE UNIQUE INDEX factory_contacts_one_primary_idx
    ON factory_contacts (tenant_id, factory_id) WHERE is_primary AND status = 'ACTIVE';

CREATE TABLE factory_owners (
    id                  BIGSERIAL PRIMARY KEY,
    tenant_id           BIGINT       NOT NULL DEFAULT 1,
    factory_id          BIGINT       NOT NULL REFERENCES factories(id) ON DELETE RESTRICT,
    employee_id         BIGINT       NOT NULL,
    employee_name       VARCHAR(100) NOT NULL,
    responsibility_code VARCHAR(50) NOT NULL,
    is_primary          BOOLEAN      NOT NULL DEFAULT false,
    start_date          DATE,
    end_date            DATE,
    status              VARCHAR(32) NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by          BIGINT      NOT NULL DEFAULT 0,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by          BIGINT      NOT NULL DEFAULT 0,
    CHECK (end_date IS NULL OR start_date IS NULL OR end_date >= start_date)
);
CREATE INDEX factory_owners_factory_idx ON factory_owners (tenant_id, factory_id, status, id);
CREATE INDEX factory_owners_employee_idx ON factory_owners (tenant_id, employee_id, status, factory_id);
CREATE UNIQUE INDEX factory_owners_active_unique_idx
    ON factory_owners (tenant_id, factory_id, employee_id, responsibility_code) WHERE status = 'ACTIVE';
CREATE UNIQUE INDEX factory_owners_one_primary_idx
    ON factory_owners (tenant_id, factory_id) WHERE is_primary AND status = 'ACTIVE';

CREATE TABLE factory_capabilities (
    id                BIGSERIAL PRIMARY KEY,
    tenant_id         BIGINT       NOT NULL DEFAULT 1,
    factory_id        BIGINT       NOT NULL REFERENCES factories(id) ON DELETE RESTRICT,
    product_category  VARCHAR(150) NOT NULL,
    process           VARCHAR(200) NOT NULL DEFAULT '',
    monthly_capacity  NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (monthly_capacity >= 0),
    capacity_unit     VARCHAR(50)  NOT NULL DEFAULT '',
    moq               NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (moq >= 0),
    lead_time_days    INT          NOT NULL DEFAULT 0 CHECK (lead_time_days >= 0),
    period_label      VARCHAR(100) NOT NULL DEFAULT '',
    confirmed_on      DATE,
    remark            TEXT         NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_by        BIGINT       NOT NULL DEFAULT 0,
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_by        BIGINT       NOT NULL DEFAULT 0
);
CREATE INDEX factory_capabilities_factory_idx ON factory_capabilities (tenant_id, factory_id, id);

CREATE TABLE factory_certificates (
    id              BIGSERIAL PRIMARY KEY,
    tenant_id       BIGINT       NOT NULL DEFAULT 1,
    factory_id      BIGINT       NOT NULL REFERENCES factories(id) ON DELETE RESTRICT,
    name            VARCHAR(200) NOT NULL,
    certificate_no  VARCHAR(100) NOT NULL DEFAULT '',
    issued_on       DATE,
    expires_on      DATE,
    status          VARCHAR(32)  NOT NULL DEFAULT 'VALID'
                    CHECK (status IN ('VALID','EXPIRING','EXPIRED','SUSPENDED')),
    file_key        VARCHAR(500) NOT NULL DEFAULT '',
    remark          TEXT         NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_by      BIGINT       NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_by      BIGINT       NOT NULL DEFAULT 0,
    CHECK (expires_on IS NULL OR issued_on IS NULL OR expires_on >= issued_on)
);
CREATE INDEX factory_certificates_factory_idx ON factory_certificates (tenant_id, factory_id, status, id);

CREATE TABLE supplier_change_logs (
    id BIGSERIAL PRIMARY KEY, tenant_id BIGINT NOT NULL DEFAULT 1,
    supplier_id BIGINT NOT NULL REFERENCES suppliers(id) ON DELETE RESTRICT,
    action VARCHAR(50) NOT NULL, section VARCHAR(50) NOT NULL,
    summary VARCHAR(300) NOT NULL DEFAULT '', before_data JSONB NOT NULL DEFAULT '{}'::jsonb,
    after_data JSONB NOT NULL DEFAULT '{}'::jsonb, operator_id BIGINT NOT NULL DEFAULT 0,
    operator_name VARCHAR(100) NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX supplier_change_logs_idx ON supplier_change_logs (tenant_id, supplier_id, created_at DESC, id DESC);

CREATE TABLE factory_change_logs (
    id BIGSERIAL PRIMARY KEY, tenant_id BIGINT NOT NULL DEFAULT 1,
    factory_id BIGINT NOT NULL REFERENCES factories(id) ON DELETE RESTRICT,
    action VARCHAR(50) NOT NULL, section VARCHAR(50) NOT NULL,
    summary VARCHAR(300) NOT NULL DEFAULT '', before_data JSONB NOT NULL DEFAULT '{}'::jsonb,
    after_data JSONB NOT NULL DEFAULT '{}'::jsonb, operator_id BIGINT NOT NULL DEFAULT 0,
    operator_name VARCHAR(100) NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX factory_change_logs_idx ON factory_change_logs (tenant_id, factory_id, created_at DESC, id DESC);

INSERT INTO option_items (tenant_id, category, code, label, sort_order) VALUES
    (1, 'SUPPLIER_BUSINESS_TYPE', 'GENERAL', '产品/材料供应商', 1),
    (1, 'SUPPLIER_BUSINESS_TYPE', 'CARRIER', '船公司/承运人', 2),
    (1, 'SUPPLIER_BUSINESS_TYPE', 'FORWARDER', '国际货运代理', 3),
    (1, 'SUPPLIER_BUSINESS_TYPE', 'CUSTOMS_BROKER', '报关服务商', 4),
    (1, 'SUPPLIER_BUSINESS_TYPE', 'WAREHOUSE', '仓储服务商', 5),
    (1, 'SUPPLIER_BUSINESS_TYPE', 'SERVICE', '其他专业服务商', 6),
    (1, 'SUPPLIER_OWNER_RESPONSIBILITY', 'PROCUREMENT', '采购', 1),
    (1, 'SUPPLIER_OWNER_RESPONSIBILITY', 'FOLLOW_UP', '跟单', 2),
    (1, 'SUPPLIER_OWNER_RESPONSIBILITY', 'FINANCE', '财务', 3),
    (1, 'SUPPLIER_OWNER_RESPONSIBILITY', 'LOGISTICS', '物流', 4),
    (1, 'FACTORY_OWNER_RESPONSIBILITY', 'PROCUREMENT', '采购', 1),
    (1, 'FACTORY_OWNER_RESPONSIBILITY', 'FOLLOW_UP', '跟单', 2),
    (1, 'FACTORY_OWNER_RESPONSIBILITY', 'QUALITY', '质检', 3),
    (1, 'FACTORY_OWNER_RESPONSIBILITY', 'LOGISTICS', '物流', 4)
ON CONFLICT (tenant_id, category, code) DO NOTHING;

INSERT INTO number_rules (tenant_id, biz_type, prefix, period, seq_len)
VALUES (1, 'FACTORY', 'FA', 'NONE', 5)
ON CONFLICT (tenant_id, biz_type) DO NOTHING;

-- +goose Down
DELETE FROM number_rules WHERE tenant_id = 1 AND biz_type = 'FACTORY';
DELETE FROM option_items WHERE tenant_id = 1 AND category IN
    ('SUPPLIER_BUSINESS_TYPE','SUPPLIER_OWNER_RESPONSIBILITY','FACTORY_OWNER_RESPONSIBILITY');
DROP TABLE IF EXISTS factory_change_logs;
DROP TABLE IF EXISTS supplier_change_logs;
DROP TABLE IF EXISTS factory_certificates;
DROP TABLE IF EXISTS factory_capabilities;
DROP TABLE IF EXISTS factory_owners;
DROP TABLE IF EXISTS factory_contacts;
DROP TABLE IF EXISTS factories;
DROP TABLE IF EXISTS supplier_owners;
DROP TABLE IF EXISTS supplier_contacts;
DROP INDEX IF EXISTS suppliers_business_types_idx;
DROP INDEX IF EXISTS suppliers_country_idx;
ALTER TABLE suppliers
    DROP COLUMN IF EXISTS business_types,
    DROP COLUMN IF EXISTS payment_term,
    DROP COLUMN IF EXISTS registered_address,
    DROP COLUMN IF EXISTS tax_id,
    DROP COLUMN IF EXISTS country_code,
    DROP COLUMN IF EXISTS short_name,
    DROP COLUMN IF EXISTS name_en,
    DROP COLUMN IF EXISTS name_zh;

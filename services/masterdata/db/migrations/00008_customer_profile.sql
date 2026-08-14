-- +goose Up

-- B2 第一步只建立客户详细资料和地址的稳定数据结构。联系人、负责人、
-- 导入和变更历史在后续小批次接入，避免一个迁移同时承担过多职责。
ALTER TABLE customers
    ADD COLUMN short_name          VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN english_name        VARCHAR(200) NOT NULL DEFAULT '',
    ADD COLUMN customer_type       VARCHAR(50)  NOT NULL DEFAULT '',
    ADD COLUMN industry            VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN source              VARCHAR(50)  NOT NULL DEFAULT '',
    ADD COLUMN tags                TEXT[]       NOT NULL DEFAULT '{}',
    ADD COLUMN website             VARCHAR(300) NOT NULL DEFAULT '',
    ADD COLUMN primary_language    VARCHAR(20)  NOT NULL DEFAULT '',
    ADD COLUMN timezone            VARCHAR(64)  NOT NULL DEFAULT '',
    ADD COLUMN registered_name     VARCHAR(200) NOT NULL DEFAULT '',
    ADD COLUMN registration_no     VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN tax_id              VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN invoice_title       VARCHAR(200) NOT NULL DEFAULT '',
    ADD COLUMN invoice_tax_no      VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN invoice_remark      TEXT         NOT NULL DEFAULT '',
    ADD COLUMN payment_days        INT          NOT NULL DEFAULT 0,
    ADD COLUMN credit_limit_minor  BIGINT       NOT NULL DEFAULT 0,
    ADD COLUMN credit_currency     CHAR(3)      NOT NULL DEFAULT 'USD',
    ADD COLUMN credit_status       VARCHAR(32)  NOT NULL DEFAULT 'NORMAL',
    ADD COLUMN business_status     VARCHAR(32)  NOT NULL DEFAULT 'PROSPECT';

ALTER TABLE customers
    ADD CONSTRAINT customers_payment_days_check CHECK (payment_days >= 0),
    ADD CONSTRAINT customers_credit_limit_check CHECK (credit_limit_minor >= 0),
    ADD CONSTRAINT customers_credit_currency_check CHECK (credit_currency ~ '^[A-Z]{3}$'),
    ADD CONSTRAINT customers_credit_status_check
        CHECK (credit_status IN ('NORMAL', 'WATCH', 'CREDIT_SUSPENDED')),
    ADD CONSTRAINT customers_business_status_check
        CHECK (business_status IN ('PROSPECT', 'COOPERATING', 'PAUSED', 'INACTIVE'));

CREATE INDEX customers_profile_filter_idx
    ON customers (tenant_id, business_status, customer_type, country_code);

CREATE TABLE customer_addresses (
    id            BIGSERIAL PRIMARY KEY,
    tenant_id     BIGINT       NOT NULL DEFAULT 1,
    customer_id   BIGINT       NOT NULL REFERENCES customers(id) ON DELETE RESTRICT,
    address_type  VARCHAR(32)  NOT NULL,
    country_code  CHAR(2)      NOT NULL DEFAULT '',
    state         VARCHAR(100) NOT NULL DEFAULT '',
    city          VARCHAR(100) NOT NULL DEFAULT '',
    postal_code   VARCHAR(30)  NOT NULL DEFAULT '',
    address_line  VARCHAR(500) NOT NULL,
    is_default    BOOLEAN      NOT NULL DEFAULT false,
    sort_order    INT          NOT NULL DEFAULT 0,
    status        VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE',
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_by    BIGINT       NOT NULL DEFAULT 0,
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_by    BIGINT       NOT NULL DEFAULT 0,
    CONSTRAINT customer_addresses_type_check
        CHECK (address_type IN ('REGISTERED', 'OFFICE', 'BILLING', 'SHIPPING')),
    CONSTRAINT customer_addresses_country_check
        CHECK (country_code = '' OR country_code ~ '^[A-Z]{2}$'),
    CONSTRAINT customer_addresses_status_check
        CHECK (status IN ('ACTIVE', 'INACTIVE'))
);

CREATE INDEX customer_addresses_customer_idx
    ON customer_addresses (tenant_id, customer_id, status, sort_order, id);

-- 每种地址类型最多一个默认地址；更新默认地址时服务会在同一事务内先取消旧默认。
CREATE UNIQUE INDEX customer_addresses_one_default_idx
    ON customer_addresses (tenant_id, customer_id, address_type)
    WHERE is_default AND status = 'ACTIVE';

INSERT INTO option_items (tenant_id, category, code, label, sort_order) VALUES
    (1, 'CUSTOMER_TYPE', 'IMPORTER',    '进口商',   1),
    (1, 'CUSTOMER_TYPE', 'DISTRIBUTOR', '经销商',   2),
    (1, 'CUSTOMER_TYPE', 'END_CUSTOMER','终端客户', 3),
    (1, 'CUSTOMER_TYPE', 'AGENT',       '代理商',   4),
    (1, 'CUSTOMER_TYPE', 'OTHER',       '其他',     5),
    (1, 'CUSTOMER_SOURCE', 'REFERRAL',  '客户转介绍', 1),
    (1, 'CUSTOMER_SOURCE', 'EXHIBITION','展会',       2),
    (1, 'CUSTOMER_SOURCE', 'WEBSITE',   '网站',       3),
    (1, 'CUSTOMER_SOURCE', 'OUTREACH',  '主动开发',   4),
    (1, 'CUSTOMER_SOURCE', 'OTHER',     '其他',       5)
ON CONFLICT (tenant_id, category, code) DO NOTHING;

-- +goose Down

DELETE FROM option_items
WHERE tenant_id = 1
  AND ((category = 'CUSTOMER_TYPE' AND code IN ('IMPORTER','DISTRIBUTOR','END_CUSTOMER','AGENT','OTHER'))
    OR (category = 'CUSTOMER_SOURCE' AND code IN ('REFERRAL','EXHIBITION','WEBSITE','OUTREACH','OTHER')));

DROP INDEX IF EXISTS customer_addresses_one_default_idx;
DROP INDEX IF EXISTS customer_addresses_customer_idx;
DROP TABLE IF EXISTS customer_addresses;
DROP INDEX IF EXISTS customers_profile_filter_idx;

ALTER TABLE customers
    DROP CONSTRAINT IF EXISTS customers_business_status_check,
    DROP CONSTRAINT IF EXISTS customers_credit_status_check,
    DROP CONSTRAINT IF EXISTS customers_credit_currency_check,
    DROP CONSTRAINT IF EXISTS customers_credit_limit_check,
    DROP CONSTRAINT IF EXISTS customers_payment_days_check,
    DROP COLUMN IF EXISTS business_status,
    DROP COLUMN IF EXISTS credit_status,
    DROP COLUMN IF EXISTS credit_currency,
    DROP COLUMN IF EXISTS credit_limit_minor,
    DROP COLUMN IF EXISTS payment_days,
    DROP COLUMN IF EXISTS invoice_remark,
    DROP COLUMN IF EXISTS invoice_tax_no,
    DROP COLUMN IF EXISTS invoice_title,
    DROP COLUMN IF EXISTS tax_id,
    DROP COLUMN IF EXISTS registration_no,
    DROP COLUMN IF EXISTS registered_name,
    DROP COLUMN IF EXISTS timezone,
    DROP COLUMN IF EXISTS primary_language,
    DROP COLUMN IF EXISTS website,
    DROP COLUMN IF EXISTS tags,
    DROP COLUMN IF EXISTS source,
    DROP COLUMN IF EXISTS industry,
    DROP COLUMN IF EXISTS customer_type,
    DROP COLUMN IF EXISTS english_name,
    DROP COLUMN IF EXISTS short_name;

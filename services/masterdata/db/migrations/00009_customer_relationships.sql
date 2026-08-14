-- +goose Up

-- B2：联系人改为可独立维护的业务资料，不再依赖“整组删除后重建”。
ALTER TABLE customer_contacts
    ADD COLUMN department          VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN mobile              VARCHAR(50)  NOT NULL DEFAULT '',
    ADD COLUMN instant_messaging   VARCHAR(200) NOT NULL DEFAULT '',
    ADD COLUMN language            VARCHAR(30)  NOT NULL DEFAULT '',
    ADD COLUMN remark              TEXT         NOT NULL DEFAULT '',
    ADD COLUMN status              VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE',
    ADD COLUMN created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    ADD COLUMN created_by          BIGINT       NOT NULL DEFAULT 0,
    ADD COLUMN updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    ADD COLUMN updated_by          BIGINT       NOT NULL DEFAULT 0,
    ADD CONSTRAINT customer_contacts_status_check CHECK (status IN ('ACTIVE', 'INACTIVE'));

-- 旧页面允许多行都勾选“主要联系人”。迁移时保留排序最靠前的一人，避免唯一索引
-- 因历史脏数据失败，同时不删除任何联系人。
WITH ranked AS (
    SELECT id, row_number() OVER (PARTITION BY tenant_id, customer_id ORDER BY sort_order, id) AS rn
    FROM customer_contacts WHERE is_primary
)
UPDATE customer_contacts SET is_primary = false
WHERE id IN (SELECT id FROM ranked WHERE rn > 1);

CREATE UNIQUE INDEX customer_contacts_one_primary_idx
    ON customer_contacts (tenant_id, customer_id)
    WHERE is_primary AND status = 'ACTIVE';

CREATE TABLE customer_owners (
    id                    BIGSERIAL PRIMARY KEY,
    tenant_id             BIGINT       NOT NULL DEFAULT 1,
    customer_id           BIGINT       NOT NULL REFERENCES customers(id) ON DELETE RESTRICT,
    employee_id           BIGINT       NOT NULL,
    employee_name         VARCHAR(100) NOT NULL,
    responsibility_code   VARCHAR(50)  NOT NULL,
    start_date            DATE,
    end_date              DATE,
    status                VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE',
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_by            BIGINT       NOT NULL DEFAULT 0,
    updated_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_by            BIGINT       NOT NULL DEFAULT 0,
    CONSTRAINT customer_owners_status_check CHECK (status IN ('ACTIVE', 'INACTIVE')),
    CONSTRAINT customer_owners_dates_check CHECK (end_date IS NULL OR start_date IS NULL OR end_date >= start_date)
);

CREATE INDEX customer_owners_customer_idx
    ON customer_owners (tenant_id, customer_id, status, id);
CREATE INDEX customer_owners_employee_idx
    ON customer_owners (tenant_id, employee_id, status, customer_id);
CREATE UNIQUE INDEX customer_owners_active_unique_idx
    ON customer_owners (tenant_id, customer_id, employee_id, responsibility_code)
    WHERE status = 'ACTIVE';

CREATE TABLE customer_change_logs (
    id             BIGSERIAL PRIMARY KEY,
    tenant_id      BIGINT       NOT NULL DEFAULT 1,
    customer_id    BIGINT       NOT NULL REFERENCES customers(id) ON DELETE RESTRICT,
    action         VARCHAR(50)  NOT NULL,
    section        VARCHAR(50)  NOT NULL,
    summary        VARCHAR(300) NOT NULL DEFAULT '',
    before_data    JSONB        NOT NULL DEFAULT '{}'::jsonb,
    after_data     JSONB        NOT NULL DEFAULT '{}'::jsonb,
    operator_id    BIGINT       NOT NULL DEFAULT 0,
    operator_name  VARCHAR(100) NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX customer_change_logs_customer_idx
    ON customer_change_logs (tenant_id, customer_id, created_at DESC, id DESC);

INSERT INTO option_items (tenant_id, category, code, label, sort_order) VALUES
    (1, 'CUSTOMER_OWNER_RESPONSIBILITY', 'SALES',            '销售',     1),
    (1, 'CUSTOMER_OWNER_RESPONSIBILITY', 'FOLLOW_UP',        '跟单',     2),
    (1, 'CUSTOMER_OWNER_RESPONSIBILITY', 'DOCUMENT',         '单证',     3),
    (1, 'CUSTOMER_OWNER_RESPONSIBILITY', 'FINANCE',          '财务',     4),
    (1, 'CUSTOMER_OWNER_RESPONSIBILITY', 'CUSTOMER_SERVICE', '客户服务', 5)
ON CONFLICT (tenant_id, category, code) DO NOTHING;

-- +goose Down

DELETE FROM option_items
WHERE tenant_id = 1 AND category = 'CUSTOMER_OWNER_RESPONSIBILITY'
  AND code IN ('SALES', 'FOLLOW_UP', 'DOCUMENT', 'FINANCE', 'CUSTOMER_SERVICE');

DROP INDEX IF EXISTS customer_change_logs_customer_idx;
DROP TABLE IF EXISTS customer_change_logs;
DROP INDEX IF EXISTS customer_owners_active_unique_idx;
DROP INDEX IF EXISTS customer_owners_employee_idx;
DROP INDEX IF EXISTS customer_owners_customer_idx;
DROP TABLE IF EXISTS customer_owners;
DROP INDEX IF EXISTS customer_contacts_one_primary_idx;

ALTER TABLE customer_contacts
    DROP CONSTRAINT IF EXISTS customer_contacts_status_check,
    DROP COLUMN IF EXISTS updated_by,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS created_by,
    DROP COLUMN IF EXISTS created_at,
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS remark,
    DROP COLUMN IF EXISTS language,
    DROP COLUMN IF EXISTS instant_messaging,
    DROP COLUMN IF EXISTS mobile,
    DROP COLUMN IF EXISTS department;

-- +goose Up

-- 港口属于基础数据：业务单据只引用港口 ID，并保留名称、代码和时区快照。
CREATE TABLE ports (
    id             BIGSERIAL PRIMARY KEY,
    tenant_id      BIGINT       NOT NULL DEFAULT 1,
    unlocode       CHAR(5)      NOT NULL,
    name_zh        VARCHAR(200) NOT NULL DEFAULT '',
    name_en        VARCHAR(200) NOT NULL,
    country_code   CHAR(2)      NOT NULL,
    city           VARCHAR(120) NOT NULL DEFAULT '',
    timezone       VARCHAR(64)  NOT NULL,
    aliases        TEXT[]       NOT NULL DEFAULT '{}',
    status         VARCHAR(16)  NOT NULL DEFAULT 'ACTIVE',
    remark         TEXT         NOT NULL DEFAULT '',
    version        INT          NOT NULL DEFAULT 1,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_by     BIGINT       NOT NULL DEFAULT 0,
    created_by_name VARCHAR(120) NOT NULL DEFAULT '',
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_by     BIGINT       NOT NULL DEFAULT 0,
    updated_by_name VARCHAR(120) NOT NULL DEFAULT '',
    CONSTRAINT ports_unlocode_format CHECK (unlocode ~ '^[A-Z]{2}[A-Z0-9]{3}$'),
    CONSTRAINT ports_country_format CHECK (country_code ~ '^[A-Z]{2}$'),
    CONSTRAINT ports_status_check CHECK (status IN ('ACTIVE', 'INACTIVE')),
    CONSTRAINT ports_tenant_unlocode_unique UNIQUE (tenant_id, unlocode)
);

CREATE INDEX ports_search_idx ON ports (tenant_id, country_code, status, name_en, id);

CREATE TABLE port_change_logs (
    id            BIGSERIAL PRIMARY KEY,
    tenant_id     BIGINT      NOT NULL,
    port_id       BIGINT      NOT NULL REFERENCES ports(id) ON DELETE RESTRICT,
    action        VARCHAR(24) NOT NULL,
    before_data   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    after_data    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    operator_id   BIGINT      NOT NULL DEFAULT 0,
    operator_name VARCHAR(120) NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX port_change_logs_port_idx ON port_change_logs (tenant_id, port_id, id DESC);

-- +goose Down
DROP TABLE port_change_logs;
DROP TABLE ports;

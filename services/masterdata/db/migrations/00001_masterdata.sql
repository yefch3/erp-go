-- +goose Up
CREATE TABLE customers (
    id           BIGSERIAL PRIMARY KEY,
    tenant_id    BIGINT       NOT NULL DEFAULT 1,
    code         VARCHAR(50)  NOT NULL,
    name         VARCHAR(200) NOT NULL,
    country      VARCHAR(100) NOT NULL DEFAULT '',
    address      VARCHAR(500) NOT NULL DEFAULT '',
    currency     CHAR(3)      NOT NULL DEFAULT 'USD',
    payment_term VARCHAR(50)  NOT NULL DEFAULT '',
    remark       TEXT         NOT NULL DEFAULT '',
    status       VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_by   BIGINT       NOT NULL DEFAULT 0,
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_by   BIGINT       NOT NULL DEFAULT 0,
    UNIQUE (tenant_id, code)
);
CREATE INDEX customers_name_idx ON customers (tenant_id, name);

CREATE TABLE customer_contacts (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   BIGINT       NOT NULL DEFAULT 1,
    customer_id BIGINT       NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    name        VARCHAR(100) NOT NULL,
    title       VARCHAR(100) NOT NULL DEFAULT '',
    email       VARCHAR(200) NOT NULL DEFAULT '',
    phone       VARCHAR(50)  NOT NULL DEFAULT '',
    is_primary  BOOLEAN      NOT NULL DEFAULT false,
    sort_order  INT          NOT NULL DEFAULT 0
);
CREATE INDEX customer_contacts_customer_idx ON customer_contacts (tenant_id, customer_id);

CREATE TABLE suppliers (
    id            BIGSERIAL PRIMARY KEY,
    tenant_id     BIGINT       NOT NULL DEFAULT 1,
    code          VARCHAR(50)  NOT NULL,
    name          VARCHAR(200) NOT NULL,
    country       VARCHAR(100) NOT NULL DEFAULT '',
    address       VARCHAR(500) NOT NULL DEFAULT '',
    currency      CHAR(3)      NOT NULL DEFAULT 'CNY',
    contact_name  VARCHAR(100) NOT NULL DEFAULT '',
    contact_phone VARCHAR(50)  NOT NULL DEFAULT '',
    contact_email VARCHAR(200) NOT NULL DEFAULT '',
    remark        TEXT         NOT NULL DEFAULT '',
    status        VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_by    BIGINT       NOT NULL DEFAULT 0,
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_by    BIGINT       NOT NULL DEFAULT 0,
    UNIQUE (tenant_id, code)
);

CREATE TABLE option_items (
    id         BIGSERIAL PRIMARY KEY,
    tenant_id  BIGINT       NOT NULL DEFAULT 1,
    category   VARCHAR(50)  NOT NULL,
    code       VARCHAR(50)  NOT NULL,
    label      VARCHAR(100) NOT NULL,
    sort_order INT          NOT NULL DEFAULT 0,
    status     VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    UNIQUE (tenant_id, category, code)
);

CREATE TABLE number_rules (
    id        BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT      NOT NULL DEFAULT 1,
    biz_type  VARCHAR(50) NOT NULL,
    prefix    VARCHAR(20) NOT NULL,
    period    VARCHAR(20) NOT NULL DEFAULT 'DAILY' CHECK (period IN ('NONE','DAILY','MONTHLY','YEARLY')),
    seq_len   INT         NOT NULL DEFAULT 4 CHECK (seq_len BETWEEN 1 AND 10),
    UNIQUE (tenant_id, biz_type)
);

-- One row per (rule, period window); the single-statement upsert in
-- queries/masterdata.sql makes concurrent NextNumber calls serialize on the
-- row lock, which is what guarantees uniqueness.
CREATE TABLE number_sequences (
    tenant_id  BIGINT      NOT NULL DEFAULT 1,
    biz_type   VARCHAR(50) NOT NULL,
    period_key VARCHAR(20) NOT NULL,
    next_seq   BIGINT      NOT NULL,
    PRIMARY KEY (tenant_id, biz_type, period_key)
);

INSERT INTO option_items (tenant_id, category, code, label, sort_order) VALUES
  (1, 'PAYMENT_METHOD', 'TT',  '电汇 T/T',        1),
  (1, 'PAYMENT_METHOD', 'LC',  '信用证 L/C',      2),
  (1, 'PAYMENT_METHOD', 'DP',  '付款交单 D/P',    3),
  (1, 'PAYMENT_METHOD', 'DA',  '承兑交单 D/A',    4),
  (1, 'TRADE_TERM', 'FOB', 'FOB 离岸价', 1),
  (1, 'TRADE_TERM', 'CIF', 'CIF 到岸价', 2),
  (1, 'TRADE_TERM', 'CFR', 'CFR 成本加运费', 3),
  (1, 'TRADE_TERM', 'EXW', 'EXW 工厂交货', 4),
  (1, 'TRADE_TERM', 'DDP', 'DDP 完税后交货', 5),
  (1, 'SHIPPING_EXCEPTION', 'DELAY',    '延误', 1),
  (1, 'SHIPPING_EXCEPTION', 'ROLLOVER', '甩柜', 2),
  (1, 'SHIPPING_EXCEPTION', 'DOC_MISSING', '单证缺失', 3);

INSERT INTO number_rules (tenant_id, biz_type, prefix, period, seq_len) VALUES
  (1, 'QUOTATION',      'QT',  'DAILY',   4),
  (1, 'CONTRACT',       'CT',  'MONTHLY', 4),
  (1, 'PURCHASE_ORDER', 'PO',  'MONTHLY', 4),
  (1, 'SHIPMENT_PLAN',  'SP',  'DAILY',   4),
  (1, 'INBOUND',        'IN',  'DAILY',   4),
  (1, 'OUTBOUND',       'OUT', 'DAILY',   4);

-- +goose Down
DROP TABLE number_sequences;
DROP TABLE number_rules;
DROP TABLE option_items;
DROP TABLE suppliers;
DROP TABLE customer_contacts;
DROP TABLE customers;

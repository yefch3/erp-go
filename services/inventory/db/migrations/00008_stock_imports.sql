-- +goose Up

CREATE TABLE stock_imports (
    id                BIGSERIAL PRIMARY KEY,
    tenant_id         BIGINT       NOT NULL,
    import_token      VARCHAR(64)  NOT NULL,
    batch_no          VARCHAR(50)  NOT NULL DEFAULT '',
    import_type       VARCHAR(32)  NOT NULL DEFAULT 'INITIAL_STOCK'
                      CHECK (import_type IN ('INITIAL_STOCK')),
    source_file_name  VARCHAR(255) NOT NULL DEFAULT '',
    file_sha256       VARCHAR(64)  NOT NULL,
    external_batch_no VARCHAR(100) NOT NULL DEFAULT '',
    template_version  VARCHAR(20)  NOT NULL,
    status            VARCHAR(20)  NOT NULL
                      CHECK (status IN ('PREVIEW', 'INVALID', 'CONFIRMED', 'CANCELLED')),
    total_count       INT          NOT NULL DEFAULT 0,
    valid_count       INT          NOT NULL DEFAULT 0,
    warning_count     INT          NOT NULL DEFAULT 0,
    error_count       INT          NOT NULL DEFAULT 0,
    duplicate_count   INT          NOT NULL DEFAULT 0,
    operator_id       BIGINT       NOT NULL DEFAULT 0,
    operator_name     VARCHAR(100) NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    confirmed_at      TIMESTAMPTZ,
    cancelled_at      TIMESTAMPTZ,
    UNIQUE (tenant_id, import_token)
);

CREATE UNIQUE INDEX stock_imports_confirmed_hash_uq
    ON stock_imports (tenant_id, file_sha256)
    WHERE status = 'CONFIRMED';
CREATE UNIQUE INDEX stock_imports_confirmed_external_batch_uq
    ON stock_imports (tenant_id, external_batch_no)
    WHERE status = 'CONFIRMED' AND external_batch_no <> '';
CREATE INDEX stock_imports_history_idx
    ON stock_imports (tenant_id, created_at DESC, id DESC);

CREATE TABLE stock_import_rows (
    id             BIGSERIAL PRIMARY KEY,
    tenant_id      BIGINT        NOT NULL,
    import_id      BIGINT        NOT NULL REFERENCES stock_imports(id) ON DELETE CASCADE,
    row_number     INT           NOT NULL,
    warehouse_id   BIGINT        NOT NULL DEFAULT 0,
    warehouse_code VARCHAR(50)   NOT NULL DEFAULT '',
    warehouse_name VARCHAR(100)  NOT NULL DEFAULT '',
    product_id     BIGINT        NOT NULL DEFAULT 0,
    product_code   VARCHAR(100)  NOT NULL DEFAULT '',
    product_name   VARCHAR(200)  NOT NULL DEFAULT '',
    sku_id         BIGINT        NOT NULL DEFAULT 0,
    sku_code       VARCHAR(100)  NOT NULL DEFAULT '',
    uom_id         BIGINT        NOT NULL DEFAULT 0,
    uom_code       VARCHAR(32)   NOT NULL DEFAULT '',
    qty            VARCHAR(50)   NOT NULL DEFAULT '',
    unit_cost      VARCHAR(50)   NOT NULL DEFAULT '',
    currency       VARCHAR(8)    NOT NULL DEFAULT '',
    remark         TEXT          NOT NULL DEFAULT '',
    verdict        VARCHAR(20)   NOT NULL CHECK (verdict IN ('VALID', 'WARNING', 'ERROR', 'DUPLICATE')),
    issue_field    VARCHAR(100)  NOT NULL DEFAULT '',
    issue_value    TEXT          NOT NULL DEFAULT '',
    issue_message  TEXT          NOT NULL DEFAULT '',
    UNIQUE (tenant_id, import_id, row_number)
);
CREATE INDEX stock_import_rows_batch_idx ON stock_import_rows (tenant_id, import_id, row_number);

-- +goose Down
DROP TABLE stock_import_rows;
DROP TABLE stock_imports;

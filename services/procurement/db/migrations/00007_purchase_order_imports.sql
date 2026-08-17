-- +goose Up

-- A preview is a durable promise about what the user reviewed. Confirmation
-- locks this row and creates the purchase order in the same transaction, so a
-- double click or a retry after a lost response cannot create a second order.
CREATE TABLE purchase_order_imports (
    id                   BIGSERIAL    PRIMARY KEY,
    tenant_id            BIGINT       NOT NULL DEFAULT 1,
    import_token         VARCHAR(64)  NOT NULL,
    source_type          VARCHAR(32)  NOT NULL CHECK (source_type IN ('MAIL_EXCEL', 'UPLOAD')),
    source_mail_id       BIGINT       NOT NULL DEFAULT 0,
    source_attachment_id BIGINT       NOT NULL DEFAULT 0,
    source_file_name     VARCHAR(255) NOT NULL DEFAULT '',
    file_sha256          VARCHAR(64)  NOT NULL DEFAULT '',
    preview_rows         JSONB        NOT NULL,
    status               VARCHAR(16)  NOT NULL DEFAULT 'PREVIEWED'
                                      CHECK (status IN ('PREVIEWED', 'CREATED', 'EXPIRED')),
    purchase_order_id    BIGINT       REFERENCES purchase_orders(id),
    created_by           BIGINT       NOT NULL,
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    expires_at           TIMESTAMPTZ  NOT NULL,
    confirmed_at         TIMESTAMPTZ,
    UNIQUE (tenant_id, import_token)
);

CREATE INDEX purchase_order_imports_source_idx
    ON purchase_order_imports (tenant_id, file_sha256, created_at DESC)
    WHERE file_sha256 <> '';
CREATE INDEX purchase_order_imports_expiry_idx
    ON purchase_order_imports (expires_at)
    WHERE status = 'PREVIEWED';

-- +goose Down
DROP TABLE purchase_order_imports;

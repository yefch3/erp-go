-- +goose Up
CREATE TABLE product_categories (
    id         BIGSERIAL    PRIMARY KEY,
    tenant_id  BIGINT       NOT NULL DEFAULT 1,
    code       VARCHAR(50)  NOT NULL,
    name       VARCHAR(100) NOT NULL,
    parent_id  BIGINT REFERENCES product_categories(id),
    -- Materialized path ("/1/4/9/") so a subtree is one LIKE, not a recursion.
    path       VARCHAR(500) NOT NULL DEFAULT '',
    level      INT          NOT NULL DEFAULT 1,
    sort_order INT          NOT NULL DEFAULT 0,
    status     VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    UNIQUE (tenant_id, code)
);

CREATE TABLE uoms (
    id        BIGSERIAL   PRIMARY KEY,
    tenant_id BIGINT      NOT NULL DEFAULT 1,
    code      VARCHAR(20) NOT NULL,
    name      VARCHAR(50) NOT NULL,
    uom_type  VARCHAR(32) NOT NULL CHECK (uom_type IN ('COUNT','WEIGHT','VOLUME','LENGTH')),
    status    VARCHAR(32) NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    UNIQUE (tenant_id, code)
);

CREATE TABLE products (
    id                 BIGSERIAL    PRIMARY KEY,
    tenant_id          BIGINT       NOT NULL DEFAULT 1,
    code               VARCHAR(100) NOT NULL,
    name               VARCHAR(200) NOT NULL,
    -- Export documents are in English, so the English name is a first-class
    -- field rather than a translation of the display name.
    name_en            VARCHAR(200) NOT NULL DEFAULT '',
    category_id        BIGINT       NOT NULL REFERENCES product_categories(id),
    product_type       VARCHAR(32)  NOT NULL DEFAULT 'FINISHED'
                       CHECK (product_type IN ('FINISHED','SEMI','MATERIAL','SERVICE')),
    brand              VARCHAR(100) NOT NULL DEFAULT '',
    base_uom_id        BIGINT       NOT NULL REFERENCES uoms(id),
    reference_price    NUMERIC(18,2),
    reference_currency CHAR(3)      NOT NULL DEFAULT 'USD',
    -- Customs and tax-refund inputs: the refund claim is computed from these.
    hs_code            VARCHAR(50)  NOT NULL DEFAULT '',
    tax_rate           NUMERIC(5,2),
    export_rebate_rate NUMERIC(5,2),
    description        TEXT         NOT NULL DEFAULT '',
    status             VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE'
                       CHECK (status IN ('DRAFT','ACTIVE','INACTIVE')),
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_by         BIGINT       NOT NULL DEFAULT 0,
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_by         BIGINT       NOT NULL DEFAULT 0,
    UNIQUE (tenant_id, code)
);
CREATE INDEX products_category_idx ON products (tenant_id, category_id);
CREATE INDEX products_name_idx ON products (tenant_id, name);

CREATE TABLE skus (
    id         BIGSERIAL    PRIMARY KEY,
    tenant_id  BIGINT       NOT NULL DEFAULT 1,
    product_id BIGINT       NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    code       VARCHAR(100) NOT NULL,
    spec       VARCHAR(500) NOT NULL DEFAULT '',
    -- Free-form variant attributes, e.g. {"color":"red","size":"XL"}.
    attributes JSONB        NOT NULL DEFAULT '{}'::jsonb,
    status     VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, code)
);
CREATE INDEX skus_product_idx ON skus (tenant_id, product_id);

CREATE TABLE product_attachments (
    id           BIGSERIAL    PRIMARY KEY,
    tenant_id    BIGINT       NOT NULL DEFAULT 1,
    product_id   BIGINT       NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    file_name    VARCHAR(255) NOT NULL,
    -- Object-storage key, never a local path: swapping MinIO for S3/OSS is
    -- then an endpoint change, not a data migration.
    file_key     VARCHAR(500) NOT NULL,
    file_size    BIGINT       NOT NULL DEFAULT 0,
    content_type VARCHAR(100) NOT NULL DEFAULT '',
    uploaded_by  BIGINT       NOT NULL DEFAULT 0,
    uploaded_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX product_attachments_product_idx ON product_attachments (tenant_id, product_id);

-- Units of measure are reference data every export document needs; seeding
-- them means a company can create its first product without setup.
INSERT INTO uoms (tenant_id, code, name, uom_type) VALUES
  (1, 'PCS', '个',   'COUNT'),
  (1, 'SET', '套',   'COUNT'),
  (1, 'CTN', '箱',   'COUNT'),
  (1, 'PR',  '双',   'COUNT'),
  (1, 'KG',  '千克', 'WEIGHT'),
  (1, 'TON', '吨',   'WEIGHT'),
  (1, 'CBM', '立方米', 'VOLUME'),
  (1, 'M',   '米',   'LENGTH');

INSERT INTO product_categories (tenant_id, code, name, path, level, sort_order) VALUES
  (1, 'DEFAULT', '未分类', '/', 1, 999);

-- +goose Down
DROP TABLE product_attachments;
DROP TABLE skus;
DROP TABLE products;
DROP TABLE uoms;
DROP TABLE product_categories;

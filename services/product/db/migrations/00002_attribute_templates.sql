-- +goose Up

-- Trigram matching is the level-0 fallback: a tenant with no template and no
-- structured values still has to get candidates back, and fuzzy name/spec
-- similarity is all there is to work with.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- What attributes a category's products have. One template per category, and
-- the template itself is data — adding an industry is inserting rows, not
-- changing the schema and shipping a release.
--
-- Hung off the category rather than the product: a template describes a kind
-- of thing ("cold-rolled coil has these attributes"), and every product in
-- the category inherits it. Attached to products it would be repeated once
-- per product.
CREATE TABLE attribute_templates (
    id          BIGSERIAL    PRIMARY KEY,
    tenant_id   BIGINT       NOT NULL DEFAULT 1,
    category_id BIGINT       NOT NULL REFERENCES product_categories(id) ON DELETE CASCADE,
    name        VARCHAR(100) NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, category_id)
);

CREATE TABLE attribute_defs (
    id          BIGSERIAL    PRIMARY KEY,
    tenant_id   BIGINT       NOT NULL DEFAULT 1,
    template_id BIGINT       NOT NULL REFERENCES attribute_templates(id) ON DELETE CASCADE,
    -- This is the JSON key in products.attributes / skus.attributes. JSONB
    -- cannot carry a foreign key, so the link is a naming contract enforced
    -- by validation on write.
    key         VARCHAR(50)  NOT NULL,
    label       VARCHAR(100) NOT NULL,
    data_type   VARCHAR(16)  NOT NULL DEFAULT 'TEXT'
        CHECK (data_type IN ('DIMENSION','NUMBER','ENUM','TEXT','RANGE')),
    unit        VARCHAR(16)  NOT NULL DEFAULT '',
    -- Allowed values for ENUM. Empty means "any text", which is how a
    -- half-configured template still works.
    enum_values JSONB        NOT NULL DEFAULT '[]'::jsonb,
    -- How the recall engine compares this field. Read at query time to build
    -- the condition, so steel and consumer goods run the same code.
    match_strategy VARCHAR(16) NOT NULL DEFAULT 'EXACT'
        CHECK (match_strategy IN ('EXACT','TOLERANCE','SYNONYM','FUZZY','OVERLAP')),
    tolerance_pct NUMERIC(6,3),
    -- Which side of the product/SKU split this attribute lives on. Shared
    -- across variants → PRODUCT; distinguishes variants → SKU. Where the
    -- line falls is a business decision, which is exactly why it is data.
    level       VARCHAR(8)   NOT NULL DEFAULT 'SKU'
        CHECK (level IN ('PRODUCT','SKU')),
    is_matchable BOOLEAN     NOT NULL DEFAULT true,
    is_required  BOOLEAN     NOT NULL DEFAULT false,
    sort_order   INT         NOT NULL DEFAULT 0,
    UNIQUE (tenant_id, template_id, key)
);
CREATE INDEX attribute_defs_template_idx ON attribute_defs (tenant_id, template_id, sort_order);

-- Product-level attribute values. Until now only skus had them, which forced
-- every shared attribute (grade, process) to be repeated on every variant.
ALTER TABLE products ADD COLUMN attributes JSONB NOT NULL DEFAULT '{}'::jsonb;
CREATE INDEX products_attrs_idx ON products USING gin (attributes);
CREATE INDEX skus_attrs_idx     ON skus     USING gin (attributes);

-- A canonical rendering of the matchable SKU-level attributes, key-sorted.
-- Two variants of the same product with identical specs are a data fault:
-- the recall engine would offer the operator two identical candidates and
-- stock would split across them. Uniqueness over a computed value needs a
-- stored column — an expression index cannot work here because which keys
-- participate is decided by the template, not by the schema.
ALTER TABLE skus ADD COLUMN attr_signature VARCHAR(500) NOT NULL DEFAULT '';

-- Partial, so rows that have no structured attributes yet are unaffected.
-- Tenants sitting at "free-text spec only" must keep working; structuring is
-- an enhancement, never a precondition.
CREATE UNIQUE INDEX skus_attr_signature_unique
    ON skus (tenant_id, product_id, attr_signature)
    WHERE attr_signature <> '';

CREATE INDEX products_name_trgm ON products USING gin (name gin_trgm_ops);
CREATE INDEX skus_spec_trgm     ON skus     USING gin (spec gin_trgm_ops);

-- +goose Down
DROP INDEX IF EXISTS skus_spec_trgm;
DROP INDEX IF EXISTS products_name_trgm;
DROP INDEX IF EXISTS skus_attr_signature_unique;
ALTER TABLE skus DROP COLUMN attr_signature;
DROP INDEX IF EXISTS skus_attrs_idx;
DROP INDEX IF EXISTS products_attrs_idx;
ALTER TABLE products DROP COLUMN attributes;
DROP TABLE attribute_defs;
DROP TABLE attribute_templates;

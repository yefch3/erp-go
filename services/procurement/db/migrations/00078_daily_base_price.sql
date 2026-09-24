-- +goose Up
CREATE TABLE daily_price_dimensions (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    kind VARCHAR(16) NOT NULL CHECK (kind IN ('PRODUCT','SUPPLIER','SPREAD')),
    master_id BIGINT,
    name VARCHAR(200) NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, kind, name)
);
CREATE UNIQUE INDEX daily_price_dimensions_master_uq
    ON daily_price_dimensions (tenant_id, kind, master_id) WHERE master_id IS NOT NULL;
CREATE INDEX daily_price_dimensions_order_idx ON daily_price_dimensions (tenant_id, kind, active, sort_order, id);

CREATE TABLE daily_base_prices (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    price_date DATE NOT NULL,
    product_id BIGINT NOT NULL,
    supplier_id BIGINT NOT NULL,
    price NUMERIC(18,4) NOT NULL CHECK (price >= 0),
    remark VARCHAR(500) NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL,
    created_by_name VARCHAR(200) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by BIGINT NOT NULL,
    updated_by_name VARCHAR(200) NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    FOREIGN KEY (tenant_id, product_id) REFERENCES daily_price_dimensions (tenant_id, id),
    FOREIGN KEY (tenant_id, supplier_id) REFERENCES daily_price_dimensions (tenant_id, id),
    UNIQUE (tenant_id, price_date, product_id, supplier_id)
);
CREATE INDEX daily_base_prices_trend_idx ON daily_base_prices (tenant_id, product_id, supplier_id, price_date);

CREATE TABLE daily_basis_spreads (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    price_date DATE NOT NULL,
    product_id BIGINT NOT NULL,
    spot NUMERIC(18,4) CHECK (spot >= 0),
    futures NUMERIC(18,4) CHECK (futures >= 0),
    created_by BIGINT NOT NULL,
    created_by_name VARCHAR(200) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by BIGINT NOT NULL,
    updated_by_name VARCHAR(200) NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    FOREIGN KEY (tenant_id, product_id) REFERENCES daily_price_dimensions (tenant_id, id),
    UNIQUE (tenant_id, price_date, product_id)
);
CREATE INDEX daily_basis_spreads_trend_idx ON daily_basis_spreads (tenant_id, product_id, price_date);

-- +goose Down
DROP TABLE daily_basis_spreads;
DROP TABLE daily_base_prices;
DROP TABLE daily_price_dimensions;

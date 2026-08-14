-- +goose Up

-- 港口的扩展资料仍属于港口主数据；码头/港区将来使用独立子表维护，避免混用层级。
ALTER TABLE ports
    ADD COLUMN port_type VARCHAR(24) NOT NULL DEFAULT 'SEAPORT',
    ADD COLUMN admin_area VARCHAR(120) NOT NULL DEFAULT '',
    ADD COLUMN latitude DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN longitude DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN has_coordinates BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN is_favorite BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE ports
    DROP CONSTRAINT ports_name_zh_required,
    DROP CONSTRAINT ports_name_en_required,
    ADD CONSTRAINT ports_name_required CHECK (btrim(name_zh) <> '' OR btrim(name_en) <> ''),
    ADD CONSTRAINT ports_type_check CHECK (port_type IN ('SEAPORT', 'RIVER_PORT', 'DRY_PORT', 'AIRPORT', 'OTHER')),
    ADD CONSTRAINT ports_latitude_check CHECK (NOT has_coordinates OR latitude BETWEEN -90 AND 90),
    ADD CONSTRAINT ports_longitude_check CHECK (NOT has_coordinates OR longitude BETWEEN -180 AND 180);

CREATE INDEX ports_favorite_idx
    ON ports (tenant_id, is_favorite DESC, country_code, unlocode);

-- +goose Down
DROP INDEX ports_favorite_idx;

ALTER TABLE ports
    DROP CONSTRAINT ports_longitude_check,
    DROP CONSTRAINT ports_latitude_check,
    DROP CONSTRAINT ports_type_check,
    DROP CONSTRAINT ports_name_required,
    ADD CONSTRAINT ports_name_zh_required CHECK (btrim(name_zh) <> ''),
    ADD CONSTRAINT ports_name_en_required CHECK (btrim(name_en) <> ''),
    DROP COLUMN is_favorite,
    DROP COLUMN has_coordinates,
    DROP COLUMN longitude,
    DROP COLUMN latitude,
    DROP COLUMN admin_area,
    DROP COLUMN port_type;

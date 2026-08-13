-- +goose Up

-- 船期引用港口主数据 ID，同时保留当时的代码、名称和时区快照。
-- 不建立跨数据库外键：masterdata 与 shipping 分属独立服务数据库。
ALTER TABLE shipping_schedules
    ADD COLUMN loading_port_id BIGINT,
    ADD COLUMN loading_port_code VARCHAR(20) NOT NULL DEFAULT '',
    ADD COLUMN loading_port_timezone VARCHAR(64) NOT NULL DEFAULT 'UTC',
    ADD COLUMN discharge_port_id BIGINT,
    ADD COLUMN discharge_port_code VARCHAR(20) NOT NULL DEFAULT '',
    ADD COLUMN discharge_port_timezone VARCHAR(64) NOT NULL DEFAULT 'UTC';

ALTER TABLE shipping_route_nodes ADD COLUMN port_id BIGINT;
CREATE INDEX shipping_schedules_port_ids_idx ON shipping_schedules (tenant_id, loading_port_id, discharge_port_id);
CREATE INDEX shipping_route_nodes_port_id_idx ON shipping_route_nodes (tenant_id, port_id) WHERE port_id IS NOT NULL;

-- +goose Down
DROP INDEX shipping_route_nodes_port_id_idx;
DROP INDEX shipping_schedules_port_ids_idx;
ALTER TABLE shipping_route_nodes DROP COLUMN port_id;
ALTER TABLE shipping_schedules
    DROP COLUMN discharge_port_timezone,
    DROP COLUMN discharge_port_code,
    DROP COLUMN discharge_port_id,
    DROP COLUMN loading_port_timezone,
    DROP COLUMN loading_port_code,
    DROP COLUMN loading_port_id;

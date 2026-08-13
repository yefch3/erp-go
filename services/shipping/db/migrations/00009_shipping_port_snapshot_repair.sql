-- +goose Up

-- 某些早期本地环境曾提前占用版本 8，但没有实际建立港口快照列。
-- 版本 9 使用 IF NOT EXISTS 修复这些环境；全新环境执行完版本 8 后本迁移为空操作。
ALTER TABLE shipping_schedules
    ADD COLUMN IF NOT EXISTS loading_port_id BIGINT,
    ADD COLUMN IF NOT EXISTS loading_port_code VARCHAR(20) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS loading_port_timezone VARCHAR(64) NOT NULL DEFAULT 'UTC',
    ADD COLUMN IF NOT EXISTS discharge_port_id BIGINT,
    ADD COLUMN IF NOT EXISTS discharge_port_code VARCHAR(20) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS discharge_port_timezone VARCHAR(64) NOT NULL DEFAULT 'UTC';

ALTER TABLE shipping_route_nodes ADD COLUMN IF NOT EXISTS port_id BIGINT;
CREATE INDEX IF NOT EXISTS shipping_schedules_port_ids_idx ON shipping_schedules (tenant_id, loading_port_id, discharge_port_id);
CREATE INDEX IF NOT EXISTS shipping_route_nodes_port_id_idx ON shipping_route_nodes (tenant_id, port_id) WHERE port_id IS NOT NULL;

-- +goose Down
-- 修复迁移不单独删除结构，实际结构生命周期由版本 8 的 Down 统一管理。
SELECT 1;

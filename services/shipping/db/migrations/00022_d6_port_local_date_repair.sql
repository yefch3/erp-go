-- +goose Up
-- D6 早期版本把带时区的港口时间直接截成 UTC 日期，午夜附近会落到前一天。
-- 这里只用各港口的“最新/实际时间”修复船期列表摘要，不改原始预计时间和路线历史。
UPDATE shipping_schedules s
SET etd = (n.latest_etd_at AT TIME ZONE COALESCE(NULLIF(n.timezone, ''), 'UTC'))::date,
    updated_at = now()
FROM shipping_route_nodes n
WHERE n.tenant_id = s.tenant_id AND n.schedule_id = s.id
  AND n.node_type = 'ORIGIN' AND n.is_active AND n.latest_etd_at IS NOT NULL
  AND s.etd IS DISTINCT FROM (n.latest_etd_at AT TIME ZONE COALESCE(NULLIF(n.timezone, ''), 'UTC'))::date;

UPDATE shipping_schedules s
SET atd = (n.actual_departure_at AT TIME ZONE COALESCE(NULLIF(n.timezone, ''), 'UTC'))::date,
    updated_at = now()
FROM shipping_route_nodes n
WHERE n.tenant_id = s.tenant_id AND n.schedule_id = s.id
  AND n.node_type = 'ORIGIN' AND n.is_active AND n.actual_departure_at IS NOT NULL
  AND s.atd IS DISTINCT FROM (n.actual_departure_at AT TIME ZONE COALESCE(NULLIF(n.timezone, ''), 'UTC'))::date;

UPDATE shipping_schedules s
SET eta = (n.latest_eta_at AT TIME ZONE COALESCE(NULLIF(n.timezone, ''), 'UTC'))::date,
    updated_at = now()
FROM shipping_route_nodes n
WHERE n.tenant_id = s.tenant_id AND n.schedule_id = s.id
  AND n.node_type = 'DESTINATION' AND n.is_active AND n.latest_eta_at IS NOT NULL
  AND s.eta IS DISTINCT FROM (n.latest_eta_at AT TIME ZONE COALESCE(NULLIF(n.timezone, ''), 'UTC'))::date;

UPDATE shipping_schedules s
SET ata = (n.actual_arrival_at AT TIME ZONE COALESCE(NULLIF(n.timezone, ''), 'UTC'))::date,
    updated_at = now()
FROM shipping_route_nodes n
WHERE n.tenant_id = s.tenant_id AND n.schedule_id = s.id
  AND n.node_type = 'DESTINATION' AND n.is_active AND n.actual_arrival_at IS NOT NULL
  AND s.ata IS DISTINCT FROM (n.actual_arrival_at AT TIME ZONE COALESCE(NULLIF(n.timezone, ''), 'UTC'))::date;

-- +goose Down
-- 数据修复不回写错误的 UTC 日期；代码回滚不应破坏已经纠正的业务摘要。
SELECT 1;

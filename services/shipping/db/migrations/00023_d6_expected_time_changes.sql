-- +goose Up
ALTER TABLE shipping_operational_alerts
    DROP CONSTRAINT shipping_operational_alerts_alert_type_check;
ALTER TABLE shipping_operational_alerts
    ADD CONSTRAINT shipping_operational_alerts_alert_type_check
        CHECK (alert_type IN ('ETA_ADVANCED','ETA_DELAYED','ETD_DELAYED','WAREHOUSE_DELAYED'));

ALTER TABLE shipping_delay_events ALTER COLUMN old_eta DROP NOT NULL;
ALTER TABLE shipping_delay_events ALTER COLUMN new_eta DROP NOT NULL;

-- 补录 D6 试用期间已经修改、但旧逻辑没有写入预计时间变更表的港口记录。
INSERT INTO shipping_delay_events (
    tenant_id, schedule_id, impact_type, affected_node_id, reason_code, reason, note,
    old_eta, new_eta, change_days, cumulative_delay_days, operator_id, operator_name
)
SELECT n.tenant_id, n.schedule_id, 'PORT', n.id, 'ETA_CHANGED',
       '历史预计到港时间变更自动补录', '',
       CASE WHEN n.original_eta_at IS NULL THEN NULL ELSE (n.original_eta_at AT TIME ZONE COALESCE(NULLIF(n.timezone,''),'UTC'))::date END,
       (n.latest_eta_at AT TIME ZONE COALESCE(NULLIF(n.timezone,''),'UTC'))::date,
       CASE WHEN n.original_eta_at IS NULL THEN 0 ELSE
         ((n.latest_eta_at AT TIME ZONE COALESCE(NULLIF(n.timezone,''),'UTC'))::date -
          (n.original_eta_at AT TIME ZONE COALESCE(NULLIF(n.timezone,''),'UTC'))::date) END,
       GREATEST(s.delay_days,0), n.updated_by, COALESCE(NULLIF(n.updated_by_name,''),'系统补录')
FROM shipping_route_nodes n
JOIN shipping_schedules s ON s.tenant_id=n.tenant_id AND s.id=n.schedule_id
WHERE n.is_active AND n.latest_eta_at IS NOT NULL
  AND n.original_eta_at IS DISTINCT FROM n.latest_eta_at
  AND NOT EXISTS (
    SELECT 1 FROM shipping_delay_events e
    WHERE e.tenant_id=n.tenant_id AND e.schedule_id=n.schedule_id
      AND e.affected_node_id=n.id AND e.reason_code='ETA_CHANGED'
  );

INSERT INTO shipping_delay_events (
    tenant_id, schedule_id, impact_type, affected_node_id, reason_code, reason, note,
    old_eta, new_eta, change_days, cumulative_delay_days, operator_id, operator_name
)
SELECT n.tenant_id, n.schedule_id, 'PORT', n.id, 'ETD_CHANGED',
       '历史预计离港时间变更自动补录', '',
       CASE WHEN n.original_etd_at IS NULL THEN NULL ELSE (n.original_etd_at AT TIME ZONE COALESCE(NULLIF(n.timezone,''),'UTC'))::date END,
       (n.latest_etd_at AT TIME ZONE COALESCE(NULLIF(n.timezone,''),'UTC'))::date,
       CASE WHEN n.original_etd_at IS NULL THEN 0 ELSE
         ((n.latest_etd_at AT TIME ZONE COALESCE(NULLIF(n.timezone,''),'UTC'))::date -
          (n.original_etd_at AT TIME ZONE COALESCE(NULLIF(n.timezone,''),'UTC'))::date) END,
       GREATEST(s.delay_days,0), n.updated_by, COALESCE(NULLIF(n.updated_by_name,''),'系统补录')
FROM shipping_route_nodes n
JOIN shipping_schedules s ON s.tenant_id=n.tenant_id AND s.id=n.schedule_id
WHERE n.is_active AND n.latest_etd_at IS NOT NULL
  AND n.original_etd_at IS DISTINCT FROM n.latest_etd_at
  AND NOT EXISTS (
    SELECT 1 FROM shipping_delay_events e
    WHERE e.tenant_id=n.tenant_id AND e.schedule_id=n.schedule_id
      AND e.affected_node_id=n.id AND e.reason_code='ETD_CHANGED'
  );

-- +goose Down
DELETE FROM shipping_operational_alerts WHERE alert_type='ETA_DELAYED';
DELETE FROM shipping_delay_events
WHERE reason IN ('历史预计到港时间变更自动补录','历史预计离港时间变更自动补录');
DELETE FROM shipping_delay_events WHERE old_eta IS NULL OR new_eta IS NULL;
ALTER TABLE shipping_delay_events ALTER COLUMN old_eta SET NOT NULL;
ALTER TABLE shipping_delay_events ALTER COLUMN new_eta SET NOT NULL;
ALTER TABLE shipping_operational_alerts
    DROP CONSTRAINT shipping_operational_alerts_alert_type_check;
ALTER TABLE shipping_operational_alerts
    ADD CONSTRAINT shipping_operational_alerts_alert_type_check
        CHECK (alert_type IN ('ETA_ADVANCED','ETD_DELAYED','WAREHOUSE_DELAYED'));

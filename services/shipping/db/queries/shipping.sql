-- name: CreateSchedule :one
INSERT INTO shipping_schedules (
    tenant_id, schedule_no, contract_id, contract_no, customer_id, customer_name,
    carrier_forwarder, vessel_name, voyage_no, port_of_loading, port_of_discharge,
    etd, atd, eta, original_eta, ata, responsible_employee_id, responsible_name, status, remark,
    created_by, created_by_name, updated_by, updated_by_name, carrier_id,
    loading_port_id, loading_port_code, loading_port_timezone,
    discharge_port_id, discharge_port_code, discharge_port_timezone
) VALUES (
    $1,
    'SCH-' || to_char(CURRENT_DATE, 'YYYYMMDD') || '-' || lpad(nextval('shipping_schedule_no_seq')::text, 6, '0'),
    $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $13, $14, $15, $16,
    'PLANNED', $17, $18, $19, $18, $19, $20, $21, $22, $23, $24, $25, $26
)
RETURNING *;

-- name: GetSchedule :one
SELECT * FROM shipping_schedules WHERE tenant_id = $1 AND id = $2;

-- name: GetScheduleForUpdate :one
SELECT * FROM shipping_schedules WHERE tenant_id = $1 AND id = $2 FOR UPDATE;

-- name: FindPossibleDuplicates :many
SELECT id, schedule_no
FROM shipping_schedules
WHERE tenant_id = $1
  AND vessel_name = $2
  AND voyage_no = $3
  AND port_of_loading = $4
  AND etd = $5
  AND status <> 'CANCELLED'
  AND id <> $6
ORDER BY updated_at DESC
LIMIT 5;

-- name: ListSchedules :many
SELECT *, count(*) OVER () AS total
FROM shipping_schedules
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (
    sqlc.arg(scope_all)::boolean
    OR responsible_employee_id = ANY(sqlc.arg(visible_employee_ids)::bigint[])
    OR COALESCE(customer_id, 0) = ANY(sqlc.arg(visible_customer_ids)::bigint[])
  )
  AND (
    sqlc.arg(keyword)::text = ''
    OR schedule_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR contract_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR customer_name ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR vessel_name ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR voyage_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR responsible_name ILIKE '%' || sqlc.arg(keyword)::text || '%'
  )
  AND (
    sqlc.arg(status)::text = ''
    OR (sqlc.arg(status)::text = 'ACTIVE' AND status NOT IN ('COMPLETED','CANCELLED'))
    OR (sqlc.arg(status)::text = 'ARCHIVED' AND status IN ('COMPLETED','CANCELLED'))
    OR status = sqlc.arg(status)::text
  )
  AND (sqlc.arg(port_of_loading)::text = '' OR port_of_loading = sqlc.arg(port_of_loading)::text)
  AND (sqlc.arg(port_of_discharge)::text = '' OR port_of_discharge = sqlc.arg(port_of_discharge)::text)
  AND (sqlc.narg(etd_from)::date IS NULL OR etd >= sqlc.narg(etd_from)::date)
  AND (sqlc.narg(etd_to)::date IS NULL OR etd <= sqlc.narg(etd_to)::date)
  AND (sqlc.narg(eta_from)::date IS NULL OR eta >= sqlc.narg(eta_from)::date)
  AND (sqlc.narg(eta_to)::date IS NULL OR eta <= sqlc.narg(eta_to)::date)
ORDER BY
  CASE WHEN eta >= CURRENT_DATE THEN 0 ELSE 1 END,
  CASE WHEN eta >= CURRENT_DATE THEN eta END ASC,
  updated_at DESC
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: CountSchedules :one
SELECT count(*)
FROM shipping_schedules
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (
    sqlc.arg(scope_all)::boolean
    OR responsible_employee_id = ANY(sqlc.arg(visible_employee_ids)::bigint[])
    OR COALESCE(customer_id, 0) = ANY(sqlc.arg(visible_customer_ids)::bigint[])
  )
  AND (
    sqlc.arg(keyword)::text = ''
    OR schedule_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR contract_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR customer_name ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR vessel_name ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR voyage_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR responsible_name ILIKE '%' || sqlc.arg(keyword)::text || '%'
  )
  AND (
    sqlc.arg(status)::text = ''
    OR (sqlc.arg(status)::text = 'ACTIVE' AND status NOT IN ('COMPLETED','CANCELLED'))
    OR (sqlc.arg(status)::text = 'ARCHIVED' AND status IN ('COMPLETED','CANCELLED'))
    OR status = sqlc.arg(status)::text
  )
  AND (sqlc.arg(port_of_loading)::text = '' OR port_of_loading = sqlc.arg(port_of_loading)::text)
  AND (sqlc.arg(port_of_discharge)::text = '' OR port_of_discharge = sqlc.arg(port_of_discharge)::text)
  AND (sqlc.narg(etd_from)::date IS NULL OR etd >= sqlc.narg(etd_from)::date)
  AND (sqlc.narg(etd_to)::date IS NULL OR etd <= sqlc.narg(etd_to)::date)
  AND (sqlc.narg(eta_from)::date IS NULL OR eta >= sqlc.narg(eta_from)::date)
  AND (sqlc.narg(eta_to)::date IS NULL OR eta <= sqlc.narg(eta_to)::date);

-- name: UpdateSchedule :one
UPDATE shipping_schedules SET
    contract_id = $3, contract_no = $4, customer_id = $5, customer_name = $6,
    carrier_forwarder = $7, vessel_name = $8, voyage_no = $9,
    port_of_loading = $10, port_of_discharge = $11,
    etd = $12, atd = $13, eta = $14, ata = $15,
    responsible_employee_id = $16, responsible_name = $17, remark = $18,
    updated_by = $19, updated_by_name = $20, updated_at = now(),
    carrier_id = $21,
    loading_port_id = $22, loading_port_code = $23, loading_port_timezone = $24,
    discharge_port_id = $25, discharge_port_code = $26, discharge_port_timezone = $27
WHERE tenant_id = $1 AND id = $2 AND status NOT IN ('COMPLETED','CANCELLED')
RETURNING *;

-- name: UpdateScheduleStatus :one
UPDATE shipping_schedules SET
    status = $3, updated_by = $4, updated_by_name = $5, updated_at = now()
WHERE tenant_id = $1 AND id = $2
RETURNING *;

-- name: AddScheduleChange :exec
INSERT INTO shipping_schedule_changes (
    tenant_id, schedule_id, change_type, field_name, old_value, new_value,
    reason, operator_id, operator_name
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9);

-- name: ListScheduleChanges :many
SELECT * FROM shipping_schedule_changes
WHERE tenant_id = $1 AND schedule_id = $2
ORDER BY created_at DESC, id DESC;

-- name: ListRouteNodes :many
SELECT * FROM shipping_route_nodes
WHERE tenant_id = $1 AND schedule_id = $2 AND is_active
ORDER BY sequence_no, id;

-- name: GetRouteNodeForUpdate :one
SELECT * FROM shipping_route_nodes
WHERE tenant_id = $1 AND schedule_id = $2 AND id = $3 AND is_active
FOR UPDATE;

-- name: InsertRouteNode :one
INSERT INTO shipping_route_nodes (
    tenant_id, schedule_id, sequence_no, node_type, port_code, port_name, timezone, port_id,
    original_eta_at, latest_eta_at, original_etd_at, latest_etd_at, remark,
    created_by, created_by_name, updated_by, updated_by_name
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9,$10,$10,$11,$12,$13,$12,$13)
RETURNING *;

-- name: SetRouteNodeSequence :exec
UPDATE shipping_route_nodes SET sequence_no = $4, updated_at = now()
WHERE tenant_id = $1 AND schedule_id = $2 AND id = $3 AND is_active;

-- name: DeactivateRouteNode :exec
UPDATE shipping_route_nodes SET
    is_active = FALSE, updated_by = $4, updated_by_name = $5, updated_at = now()
WHERE tenant_id = $1 AND schedule_id = $2 AND id = $3 AND is_active;

-- name: SetRouteNodeArrival :one
UPDATE shipping_route_nodes SET
    actual_arrival_at = $4, node_status = 'ARRIVED',
    updated_by = $5, updated_by_name = $6, updated_at = now()
WHERE tenant_id = $1 AND schedule_id = $2 AND id = $3 AND is_active
RETURNING *;

-- name: SetRouteNodeDeparture :one
UPDATE shipping_route_nodes SET
    actual_departure_at = $4, node_status = 'DEPARTED',
    updated_by = $5, updated_by_name = $6, updated_at = now()
WHERE tenant_id = $1 AND schedule_id = $2 AND id = $3 AND is_active
RETURNING *;

-- name: SetRouteNodeApproaching :one
UPDATE shipping_route_nodes SET
    node_status = 'APPROACHING', updated_by = $4, updated_by_name = $5, updated_at = now()
WHERE tenant_id = $1 AND schedule_id = $2 AND id = $3 AND is_active
RETURNING *;

-- name: SetRouteNodeSkipped :one
UPDATE shipping_route_nodes SET
    node_status = 'SKIPPED', updated_by = $4, updated_by_name = $5, updated_at = now()
WHERE tenant_id = $1 AND schedule_id = $2 AND id = $3 AND is_active
RETURNING *;

-- name: ResetOtherApproachingRouteNodes :exec
UPDATE shipping_route_nodes SET
    node_status = CASE
      WHEN actual_departure_at IS NOT NULL THEN 'DEPARTED'
      WHEN actual_arrival_at IS NOT NULL THEN 'ARRIVED'
      ELSE 'PLANNED'
    END,
    updated_at = now()
WHERE tenant_id = $1 AND schedule_id = $2 AND id <> $3
  AND is_active AND node_status = 'APPROACHING';

-- name: UpdateRouteNodeTimes :one
UPDATE shipping_route_nodes SET
    latest_eta_at = $4,
    latest_etd_at = $5,
    actual_arrival_at = $6,
    actual_departure_at = $7,
    node_status = CASE
      WHEN $7::timestamptz IS NOT NULL THEN 'DEPARTED'
      WHEN $6::timestamptz IS NOT NULL THEN 'ARRIVED'
      WHEN node_status IN ('ARRIVED','DEPARTED') THEN 'PLANNED'
      ELSE node_status
    END,
    updated_by = $8, updated_by_name = $9, updated_at = now()
WHERE tenant_id = $1 AND schedule_id = $2 AND id = $3 AND is_active
RETURNING *;

-- name: SetDestinationETA :one
UPDATE shipping_route_nodes SET
    latest_eta_at = $4, updated_by = $5, updated_by_name = $6, updated_at = now()
WHERE tenant_id = $1 AND schedule_id = $2 AND id = $3
  AND is_active AND node_type = 'DESTINATION'
RETURNING *;

-- name: UpdateScheduleProgress :one
UPDATE shipping_schedules SET
    current_route_node_id = $3, current_progress = $4,
    latest_progress_at = now(), updated_by = $5, updated_by_name = $6, updated_at = now()
WHERE tenant_id = $1 AND id = $2
RETURNING *;

-- name: SetScheduleATD :one
UPDATE shipping_schedules SET
    atd = $3, updated_by = $4, updated_by_name = $5, updated_at = now()
WHERE tenant_id = $1 AND id = $2
RETURNING *;

-- name: SetScheduleATDNullable :one
UPDATE shipping_schedules SET
    atd = sqlc.narg(atd)::date,
    updated_by = sqlc.arg(updated_by), updated_by_name = sqlc.arg(updated_by_name), updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id)
RETURNING *;

-- name: SetScheduleATA :one
UPDATE shipping_schedules SET
    ata = $3,
    delay_days = GREATEST(0, $3 - original_eta),
    updated_by = $4, updated_by_name = $5, updated_at = now()
WHERE tenant_id = $1 AND id = $2
RETURNING *;

-- name: SetScheduleATANullable :one
UPDATE shipping_schedules SET
    ata = sqlc.narg(ata)::date,
    delay_days = GREATEST(0, COALESCE(sqlc.narg(ata)::date, eta) - original_eta),
    updated_by = sqlc.arg(updated_by), updated_by_name = sqlc.arg(updated_by_name), updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id)
RETURNING *;

-- name: SetScheduleETD :one
UPDATE shipping_schedules SET
    etd = $3, updated_by = $4, updated_by_name = $5, updated_at = now()
WHERE tenant_id = $1 AND id = $2
RETURNING *;

-- name: UpdateScheduleETA :one
UPDATE shipping_schedules SET
    eta = $3,
    eta_revision = eta_revision + 1,
    delay_days = GREATEST(0, $3 - original_eta),
    updated_by = $4, updated_by_name = $5, updated_at = now()
WHERE tenant_id = $1 AND id = $2
RETURNING *;

-- name: BumpRouteVersion :one
UPDATE shipping_schedules s SET
    route_version = s.route_version + 1,
    has_temporary_call = EXISTS (
      SELECT 1 FROM shipping_route_nodes n
      WHERE n.tenant_id = s.tenant_id
        AND n.schedule_id = s.id
        AND n.is_active AND n.node_type = 'TEMPORARY'
    ),
    updated_by = $3, updated_by_name = $4, updated_at = now()
WHERE s.tenant_id = $1 AND s.id = $2
RETURNING route_version;

-- name: AddDelayEvent :one
INSERT INTO shipping_delay_events (
    tenant_id, schedule_id, impact_type, affected_node_id, from_node_id, to_node_id,
    reason_code, reason, note, old_eta, new_eta, change_days, cumulative_delay_days,
    operator_id, operator_name
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
RETURNING *;

-- name: ListDelayEvents :many
SELECT * FROM shipping_delay_events
WHERE tenant_id = $1 AND schedule_id = $2
ORDER BY created_at DESC, id DESC;

-- name: CancelPendingReminders :exec
UPDATE shipping_arrival_reminders SET status = 'CANCELLED', updated_at = now()
WHERE tenant_id = $1 AND schedule_id = $2 AND status IN ('PENDING','FAILED','PROCESSING');

-- name: CreateArrivalReminder :exec
INSERT INTO shipping_arrival_reminders (
    tenant_id, schedule_id, destination_node_id, recipient_employee_id,
    reminder_type, eta_revision, target_eta, due_at
) VALUES ($1,$2,$3,$4,'ARRIVAL_' || sqlc.arg(lead_days)::int || 'D',$5,$6,($6::date - sqlc.arg(lead_days)::int)::timestamp AT TIME ZONE 'UTC')
ON CONFLICT (tenant_id, schedule_id, destination_node_id, recipient_employee_id, reminder_type, eta_revision)
DO UPDATE SET target_eta = EXCLUDED.target_eta, due_at = EXCLUDED.due_at,
    status = 'PENDING', sent_at = NULL, read_at = NULL, attempt_count = 0,
    last_error = '', next_retry_at = NULL, updated_at = now()
WHERE shipping_arrival_reminders.status <> 'SENT';

-- name: ListArrivalReminderRules :many
SELECT lead_days FROM shipping_arrival_reminder_rules
WHERE tenant_id = $1 AND schedule_id = $2
ORDER BY lead_days DESC;

-- name: DeleteArrivalReminderRules :exec
DELETE FROM shipping_arrival_reminder_rules
WHERE tenant_id = $1 AND schedule_id = $2;

-- name: CreateArrivalReminderRule :exec
INSERT INTO shipping_arrival_reminder_rules (
    tenant_id, schedule_id, lead_days, created_by, created_by_name
) VALUES ($1,$2,$3,$4,$5)
ON CONFLICT DO NOTHING;

-- name: ListArrivalReminders :many
SELECT * FROM shipping_arrival_reminders
WHERE tenant_id = $1 AND schedule_id = $2
ORDER BY eta_revision DESC, id DESC;

-- name: ListDueArrivalReminderIDs :many
SELECT r.id
FROM shipping_arrival_reminders r
JOIN shipping_schedules s
  ON s.tenant_id = r.tenant_id AND s.id = r.schedule_id
WHERE r.status IN ('PENDING','FAILED')
  AND COALESCE(r.next_retry_at, r.due_at) <= now()
  AND s.status NOT IN ('ARRIVED','COMPLETED','CANCELLED')
ORDER BY COALESCE(r.next_retry_at, r.due_at), r.id
LIMIT $1;

-- name: GetDueArrivalReminderForUpdate :one
SELECT r.*, s.schedule_no, s.contract_no, s.customer_name, s.vessel_name,
       s.voyage_no, s.port_of_discharge, s.status AS schedule_status
FROM shipping_arrival_reminders r
JOIN shipping_schedules s
  ON s.tenant_id = r.tenant_id AND s.id = r.schedule_id
WHERE r.id = $1
  AND r.status IN ('PENDING','FAILED')
  AND COALESCE(r.next_retry_at, r.due_at) <= now()
  AND s.status NOT IN ('ARRIVED','COMPLETED','CANCELLED')
FOR UPDATE OF r;

-- name: MarkArrivalReminderSent :one
UPDATE shipping_arrival_reminders
SET status = 'SENT', title = $2, content = $3, detail_url = $4,
    sent_at = now(), attempt_count = attempt_count + 1,
    last_error = '', next_retry_at = NULL, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: MarkArrivalReminderFailed :exec
UPDATE shipping_arrival_reminders
SET status = 'FAILED', attempt_count = attempt_count + 1,
    last_error = $2, next_retry_at = $3, updated_at = now()
WHERE id = $1;

-- name: CancelIneligibleArrivalReminders :exec
UPDATE shipping_arrival_reminders r
SET status = 'CANCELLED', updated_at = now()
FROM shipping_schedules s
WHERE s.tenant_id = r.tenant_id AND s.id = r.schedule_id
  AND r.status IN ('PENDING','FAILED','PROCESSING')
  AND s.status IN ('ARRIVED','COMPLETED','CANCELLED');

-- name: ListEmployeeArrivalNotifications :many
SELECT * FROM shipping_arrival_reminders
WHERE tenant_id = $1 AND recipient_employee_id = $2 AND status = 'SENT'
  AND (NOT sqlc.arg(unread_only)::boolean OR read_at IS NULL)
ORDER BY sent_at DESC, id DESC
LIMIT 50;

-- name: CountUnreadEmployeeArrivalNotifications :one
SELECT count(*)::bigint FROM shipping_arrival_reminders
WHERE tenant_id = $1 AND recipient_employee_id = $2
  AND status = 'SENT' AND read_at IS NULL;

-- name: MarkEmployeeArrivalReminderRead :one
UPDATE shipping_arrival_reminders
SET read_at = COALESCE(read_at, now()), updated_at = now()
WHERE tenant_id = $1 AND recipient_employee_id = $2 AND id = $3 AND status = 'SENT'
RETURNING *;

-- name: DeleteExpiredEmployeeArrivalReminders :many
DELETE FROM shipping_arrival_reminders r
USING shipping_schedules s
WHERE r.tenant_id = $1 AND r.recipient_employee_id = $2
  AND r.status = 'SENT'
  AND s.tenant_id = r.tenant_id AND s.id = r.schedule_id
  AND (r.target_eta < CURRENT_DATE OR s.status IN ('ARRIVED','COMPLETED','CANCELLED'))
RETURNING r.id;

-- name: ShippingStatistics :one
SELECT
  count(*) FILTER (WHERE status IN ('SAILED','IN_TRANSIT','DELAYED'))::bigint AS in_transit,
  count(*) FILTER (WHERE status NOT IN ('ARRIVED','COMPLETED','CANCELLED') AND eta BETWEEN CURRENT_DATE AND CURRENT_DATE + 7)::bigint AS arriving_within_7_days,
  count(*) FILTER (WHERE delay_days > 0 AND status <> 'CANCELLED')::bigint AS delayed,
  count(*) FILTER (WHERE has_temporary_call AND status <> 'CANCELLED')::bigint AS temporary_call
FROM shipping_schedules
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (
    sqlc.arg(scope_all)::boolean
    OR responsible_employee_id = ANY(sqlc.arg(visible_employee_ids)::bigint[])
    OR COALESCE(customer_id, 0) = ANY(sqlc.arg(visible_customer_ids)::bigint[])
  );

-- name: CreateShippingDocument :one
INSERT INTO shipping_documents (
    tenant_id, schedule_id, document_group_key, category, version,
    file_name, file_key, file_size, content_type, remark,
    uploaded_by, uploaded_by_name
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
RETURNING *;

-- name: GetShippingDocument :one
SELECT * FROM shipping_documents
WHERE tenant_id = $1 AND schedule_id = $2 AND id = $3;

-- name: GetShippingDocumentForUpdate :one
SELECT * FROM shipping_documents
WHERE tenant_id = $1 AND schedule_id = $2 AND id = $3
FOR UPDATE;

-- name: NextShippingDocumentVersion :one
SELECT (COALESCE(MAX(version), 0) + 1)::int
FROM shipping_documents
WHERE tenant_id = $1 AND schedule_id = $2 AND document_group_key = $3;

-- name: ListShippingDocuments :many
SELECT * FROM shipping_documents
WHERE tenant_id = $1 AND schedule_id = $2
ORDER BY uploaded_at DESC, id DESC;

-- name: InvalidateShippingDocument :one
UPDATE shipping_documents SET
    status = 'VOIDED',
    voided_by = $4,
    voided_by_name = $5,
    voided_at = now(),
    void_reason = $6
WHERE tenant_id = $1 AND schedule_id = $2 AND id = $3 AND status = 'ACTIVE'
RETURNING *;

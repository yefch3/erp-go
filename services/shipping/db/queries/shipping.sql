-- name: CreateSchedule :one
INSERT INTO shipping_schedules (
    tenant_id, schedule_no, contract_id, contract_no, customer_id, customer_name,
    carrier_forwarder, vessel_name, voyage_no, port_of_loading, port_of_discharge,
    etd, atd, eta, ata, responsible_employee_id, responsible_name, status, remark,
    created_by, created_by_name, updated_by, updated_by_name
) VALUES (
    $1,
    'SCH-' || to_char(CURRENT_DATE, 'YYYYMMDD') || '-' || lpad(nextval('shipping_schedule_no_seq')::text, 6, '0'),
    $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
    'PLANNED', $17, $18, $19, $18, $19
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
    sqlc.arg(keyword)::text = ''
    OR schedule_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR contract_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR customer_name ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR vessel_name ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR voyage_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR responsible_name ILIKE '%' || sqlc.arg(keyword)::text || '%'
  )
  AND (sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text)
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
    sqlc.arg(keyword)::text = ''
    OR schedule_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR contract_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR customer_name ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR vessel_name ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR voyage_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR responsible_name ILIKE '%' || sqlc.arg(keyword)::text || '%'
  )
  AND (sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text)
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
    updated_by = $19, updated_by_name = $20, updated_at = now()
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

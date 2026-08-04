-- +goose Up

ALTER TABLE shipping_schedules
    ADD COLUMN original_eta DATE,
    ADD COLUMN eta_revision INT NOT NULL DEFAULT 1,
    ADD COLUMN route_version INT NOT NULL DEFAULT 1,
    ADD COLUMN delay_days INT NOT NULL DEFAULT 0 CHECK (delay_days >= 0),
    ADD COLUMN has_temporary_call BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN current_progress VARCHAR(200) NOT NULL DEFAULT '',
    ADD COLUMN latest_progress_at TIMESTAMPTZ;

UPDATE shipping_schedules s
SET original_eta = COALESCE((
    SELECT c.old_value::date
    FROM shipping_schedule_changes c
    WHERE c.tenant_id = s.tenant_id AND c.schedule_id = s.id
      AND c.field_name IN ('ETA','latest_eta')
      AND c.old_value ~ '^\\d{4}-\\d{2}-\\d{2}$'
    ORDER BY c.created_at, c.id
    LIMIT 1
), s.eta)
WHERE s.original_eta IS NULL;
UPDATE shipping_schedules SET delay_days = GREATEST(0, eta - original_eta);
ALTER TABLE shipping_schedules ALTER COLUMN original_eta SET NOT NULL;

CREATE TABLE shipping_route_nodes (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL DEFAULT 1,
    schedule_id BIGINT NOT NULL REFERENCES shipping_schedules(id),
    sequence_no INT NOT NULL CHECK (sequence_no > 0),
    node_type VARCHAR(20) NOT NULL
        CHECK (node_type IN ('ORIGIN','TRANSIT','TEMPORARY','DESTINATION')),
    port_code VARCHAR(20) NOT NULL DEFAULT '',
    port_name VARCHAR(200) NOT NULL,
    timezone VARCHAR(64) NOT NULL DEFAULT 'UTC',
    original_eta_at TIMESTAMPTZ,
    latest_eta_at TIMESTAMPTZ,
    actual_arrival_at TIMESTAMPTZ,
    original_etd_at TIMESTAMPTZ,
    latest_etd_at TIMESTAMPTZ,
    actual_departure_at TIMESTAMPTZ,
    node_status VARCHAR(20) NOT NULL DEFAULT 'PLANNED'
        CHECK (node_status IN ('PLANNED','APPROACHING','ARRIVED','DEPARTED','SKIPPED')),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    remark TEXT NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL,
    created_by_name VARCHAR(100) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by BIGINT NOT NULL,
    updated_by_name VARCHAR(100) NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (actual_arrival_at IS NULL OR actual_departure_at IS NULL OR actual_departure_at >= actual_arrival_at)
);

CREATE UNIQUE INDEX shipping_route_nodes_sequence_idx
    ON shipping_route_nodes (tenant_id, schedule_id, sequence_no) WHERE is_active;
CREATE UNIQUE INDEX shipping_route_nodes_origin_idx
    ON shipping_route_nodes (tenant_id, schedule_id) WHERE is_active AND node_type = 'ORIGIN';
CREATE UNIQUE INDEX shipping_route_nodes_destination_idx
    ON shipping_route_nodes (tenant_id, schedule_id) WHERE is_active AND node_type = 'DESTINATION';
CREATE INDEX shipping_route_nodes_schedule_idx
    ON shipping_route_nodes (tenant_id, schedule_id, is_active, sequence_no);

INSERT INTO shipping_route_nodes (
    tenant_id, schedule_id, sequence_no, node_type, port_name,
    original_etd_at, latest_etd_at, node_status,
    created_by, created_by_name, updated_by, updated_by_name
)
SELECT tenant_id, id, 1, 'ORIGIN', port_of_loading,
       etd::timestamp AT TIME ZONE 'UTC', etd::timestamp AT TIME ZONE 'UTC',
       CASE WHEN atd IS NOT NULL THEN 'DEPARTED' ELSE 'PLANNED' END,
       created_by, created_by_name, updated_by, updated_by_name
FROM shipping_schedules;

INSERT INTO shipping_route_nodes (
    tenant_id, schedule_id, sequence_no, node_type, port_name,
    original_eta_at, latest_eta_at, actual_arrival_at, node_status,
    created_by, created_by_name, updated_by, updated_by_name
)
SELECT tenant_id, id, 2, 'DESTINATION', port_of_discharge,
       original_eta::timestamp AT TIME ZONE 'UTC', eta::timestamp AT TIME ZONE 'UTC',
       CASE WHEN ata IS NULL THEN NULL ELSE ata::timestamp AT TIME ZONE 'UTC' END,
       CASE WHEN ata IS NOT NULL THEN 'ARRIVED' ELSE 'PLANNED' END,
       created_by, created_by_name, updated_by, updated_by_name
FROM shipping_schedules;

ALTER TABLE shipping_schedules
    ADD COLUMN current_route_node_id BIGINT REFERENCES shipping_route_nodes(id);

UPDATE shipping_schedules s
SET current_route_node_id = n.id,
    current_progress = CASE
        WHEN s.status IN ('ARRIVED','COMPLETED') THEN '已抵达 ' || s.port_of_discharge
        WHEN s.status = 'PLANNED' THEN '等待离开 ' || s.port_of_loading
        ELSE '驶往 ' || s.port_of_discharge
    END
FROM shipping_route_nodes n
WHERE n.schedule_id = s.id AND n.tenant_id = s.tenant_id
  AND n.node_type = CASE WHEN s.status = 'PLANNED' THEN 'ORIGIN' ELSE 'DESTINATION' END;

CREATE TABLE shipping_delay_events (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL DEFAULT 1,
    schedule_id BIGINT NOT NULL REFERENCES shipping_schedules(id),
    impact_type VARCHAR(20) NOT NULL CHECK (impact_type IN ('SCHEDULE','PORT','LEG')),
    affected_node_id BIGINT REFERENCES shipping_route_nodes(id),
    from_node_id BIGINT REFERENCES shipping_route_nodes(id),
    to_node_id BIGINT REFERENCES shipping_route_nodes(id),
    reason_code VARCHAR(32) NOT NULL DEFAULT 'OTHER',
    reason TEXT NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    old_eta DATE NOT NULL,
    new_eta DATE NOT NULL,
    change_days INT NOT NULL,
    cumulative_delay_days INT NOT NULL CHECK (cumulative_delay_days >= 0),
    status VARCHAR(16) NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','RESOLVED')),
    operator_id BIGINT NOT NULL,
    operator_name VARCHAR(100) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    CHECK (
      (impact_type = 'SCHEDULE') OR
      (impact_type = 'PORT' AND affected_node_id IS NOT NULL) OR
      (impact_type = 'LEG' AND from_node_id IS NOT NULL AND to_node_id IS NOT NULL)
    )
);
CREATE INDEX shipping_delay_events_schedule_idx
    ON shipping_delay_events (tenant_id, schedule_id, created_at DESC);

CREATE TABLE shipping_arrival_reminders (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL DEFAULT 1,
    schedule_id BIGINT NOT NULL REFERENCES shipping_schedules(id),
    destination_node_id BIGINT NOT NULL REFERENCES shipping_route_nodes(id),
    recipient_employee_id BIGINT NOT NULL,
    reminder_type VARCHAR(20) NOT NULL DEFAULT 'ARRIVAL_7D' CHECK (reminder_type = 'ARRIVAL_7D'),
    eta_revision INT NOT NULL,
    target_eta DATE NOT NULL,
    due_at TIMESTAMPTZ NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'PENDING'
        CHECK (status IN ('PENDING','SENT','CANCELLED','FAILED')),
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, schedule_id, destination_node_id, recipient_employee_id, reminder_type, eta_revision)
);
CREATE INDEX shipping_arrival_reminders_due_idx
    ON shipping_arrival_reminders (tenant_id, due_at) WHERE status = 'PENDING';

INSERT INTO shipping_arrival_reminders (
    tenant_id, schedule_id, destination_node_id, recipient_employee_id,
    eta_revision, target_eta, due_at
)
SELECT s.tenant_id, s.id, n.id, s.responsible_employee_id,
       s.eta_revision, s.eta, (s.eta - 7)::timestamp AT TIME ZONE 'UTC'
FROM shipping_schedules s
JOIN shipping_route_nodes n ON n.schedule_id = s.id AND n.tenant_id = s.tenant_id
WHERE n.node_type = 'DESTINATION' AND n.is_active
  AND s.status NOT IN ('ARRIVED','COMPLETED','CANCELLED');

ALTER TABLE shipping_schedule_changes
    DROP CONSTRAINT shipping_schedule_changes_change_type_check;
ALTER TABLE shipping_schedule_changes
    ADD CONSTRAINT shipping_schedule_changes_change_type_check
        CHECK (change_type IN ('DATE','ETA','STATUS','CANCEL','VESSEL_VOYAGE','ROUTE','TEMPORARY_CALL','PROGRESS','DELAY')),
    ADD COLUMN entity_type VARCHAR(20) NOT NULL DEFAULT 'SCHEDULE',
    ADD COLUMN entity_id BIGINT,
    ADD COLUMN old_value_json JSONB,
    ADD COLUMN new_value_json JSONB,
    ADD COLUMN route_version INT;

-- +goose Down
ALTER TABLE shipping_schedule_changes
    DROP COLUMN route_version,
    DROP COLUMN new_value_json,
    DROP COLUMN old_value_json,
    DROP COLUMN entity_id,
    DROP COLUMN entity_type,
    DROP CONSTRAINT shipping_schedule_changes_change_type_check;
ALTER TABLE shipping_schedule_changes
    ADD CONSTRAINT shipping_schedule_changes_change_type_check
        CHECK (change_type IN ('DATE','STATUS','CANCEL'));

DROP TABLE shipping_arrival_reminders;
DROP TABLE shipping_delay_events;
ALTER TABLE shipping_schedules DROP COLUMN current_route_node_id;
DROP TABLE shipping_route_nodes;
ALTER TABLE shipping_schedules
    DROP COLUMN latest_progress_at,
    DROP COLUMN current_progress,
    DROP COLUMN has_temporary_call,
    DROP COLUMN delay_days,
    DROP COLUMN route_version,
    DROP COLUMN eta_revision,
    DROP COLUMN original_eta;

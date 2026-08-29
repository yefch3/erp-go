-- +goose Up

CREATE TABLE sourcing_procurement_participants (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    case_id BIGINT NOT NULL REFERENCES sourcing_cases(id) ON DELETE CASCADE,
    employee_id BIGINT NOT NULL,
    employee_name TEXT NOT NULL DEFAULT '',
    participant_role TEXT NOT NULL DEFAULT 'COLLABORATOR',
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    primary_requested_at TIMESTAMPTZ,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT sourcing_procurement_participants_role_check
        CHECK (participant_role IN ('PRIMARY', 'COLLABORATOR')),
    CONSTRAINT sourcing_procurement_participants_status_check
        CHECK (status IN ('ACTIVE', 'INACTIVE')),
    CONSTRAINT sourcing_procurement_participants_employee_unique
        UNIQUE (tenant_id, case_id, employee_id)
);

CREATE UNIQUE INDEX sourcing_procurement_participants_one_primary_idx
    ON sourcing_procurement_participants (tenant_id, case_id)
    WHERE participant_role = 'PRIMARY' AND status = 'ACTIVE';

CREATE INDEX sourcing_procurement_participants_case_idx
    ON sourcing_procurement_participants (tenant_id, case_id, joined_at, id);

-- Existing accepted cases already have a primary buyer. Backfill them so the
-- new participant panel and the old durable handoff fields agree immediately.
INSERT INTO sourcing_procurement_participants (
    tenant_id, case_id, employee_id, employee_name, participant_role, joined_at
)
SELECT tenant_id, id, accepted_by, accepted_by_name, 'PRIMARY', coalesce(accepted_at, now())
FROM sourcing_cases
WHERE accepted_by IS NOT NULL
ON CONFLICT (tenant_id, case_id, employee_id) DO NOTHING;

-- +goose Down

DROP TABLE IF EXISTS sourcing_procurement_participants;

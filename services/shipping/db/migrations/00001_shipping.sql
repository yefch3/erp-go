-- +goose Up

-- The shipping schedule is the planning and tracking record for one voyage.
-- It is distinct from export.shipments, which records goods physically loaded.
CREATE TABLE shipping_schedules (
    id                      BIGSERIAL     PRIMARY KEY,
    tenant_id               BIGINT        NOT NULL DEFAULT 1,
    schedule_no             VARCHAR(50)   NOT NULL,
    contract_id             BIGINT,
    contract_no             VARCHAR(50)   NOT NULL DEFAULT '',
    customer_id             BIGINT,
    customer_name           VARCHAR(200)  NOT NULL DEFAULT '',
    carrier_forwarder       VARCHAR(200)  NOT NULL DEFAULT '',
    vessel_name             VARCHAR(200)  NOT NULL,
    voyage_no               VARCHAR(100)  NOT NULL,
    port_of_loading         VARCHAR(200)  NOT NULL,
    port_of_discharge       VARCHAR(200)  NOT NULL,
    etd                     DATE          NOT NULL,
    atd                     DATE,
    eta                     DATE          NOT NULL,
    ata                     DATE,
    responsible_employee_id BIGINT        NOT NULL,
    responsible_name        VARCHAR(100)  NOT NULL DEFAULT '',
    status                  VARCHAR(32)   NOT NULL DEFAULT 'PLANNED'
        CHECK (status IN ('PLANNED','SAILED','IN_TRANSIT','ARRIVED','COMPLETED','DELAYED','CANCELLED')),
    remark                  TEXT          NOT NULL DEFAULT '',
    created_by              BIGINT        NOT NULL,
    created_by_name         VARCHAR(100)  NOT NULL DEFAULT '',
    created_at              TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_by              BIGINT        NOT NULL,
    updated_by_name         VARCHAR(100)  NOT NULL DEFAULT '',
    updated_at              TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT shipping_schedules_dates_check CHECK (eta >= etd),
    UNIQUE (tenant_id, schedule_no)
);

CREATE INDEX shipping_schedules_eta_idx
    ON shipping_schedules (tenant_id, eta, updated_at DESC);
CREATE INDEX shipping_schedules_responsible_idx
    ON shipping_schedules (tenant_id, responsible_employee_id, status, eta);
CREATE INDEX shipping_schedules_contract_idx
    ON shipping_schedules (tenant_id, contract_no) WHERE contract_no <> '';
CREATE INDEX shipping_schedules_duplicate_hint_idx
    ON shipping_schedules (tenant_id, vessel_name, voyage_no, port_of_loading, etd);

-- Every service owns an outbox from day one. D0 creates the transport-safe
-- boundary; no shipping events are published until a later business stage.
CREATE TABLE outbox_events (
    id             BIGSERIAL   PRIMARY KEY,
    tenant_id      BIGINT      NOT NULL DEFAULT 1,
    aggregate_type TEXT        NOT NULL,
    aggregate_id   TEXT        NOT NULL,
    event_type     TEXT        NOT NULL,
    payload        JSONB       NOT NULL,
    trace_id       TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at   TIMESTAMPTZ,
    attempts       INT         NOT NULL DEFAULT 0,
    last_error     TEXT
);
CREATE INDEX outbox_events_unpublished_idx
    ON outbox_events (tenant_id, created_at) WHERE published_at IS NULL;

-- +goose Down
DROP TABLE outbox_events;
DROP TABLE shipping_schedules;

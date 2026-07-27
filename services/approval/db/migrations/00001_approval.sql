-- +goose Up
-- The approval engine knows nothing about contracts, purchase orders or
-- payments: a document is a (biz_type, biz_id, biz_no, summary) tuple.
CREATE TABLE approval_definitions (
    id         BIGSERIAL PRIMARY KEY,
    tenant_id  BIGINT       NOT NULL DEFAULT 1,
    biz_type   VARCHAR(50)  NOT NULL,
    name       VARCHAR(100) NOT NULL,
    version    INT          NOT NULL DEFAULT 1,
    status     VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('DRAFT','ACTIVE','INACTIVE')),
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, biz_type, version)
);

CREATE TABLE approval_nodes (
    id            BIGSERIAL    PRIMARY KEY,
    tenant_id     BIGINT       NOT NULL DEFAULT 1,
    definition_id BIGINT       NOT NULL REFERENCES approval_definitions(id) ON DELETE CASCADE,
    seq           INT          NOT NULL,
    name          VARCHAR(100) NOT NULL,
    -- ROLE resolves to every active holder of approver_ref at submit time;
    -- EMPLOYEE names one person.
    approver_type VARCHAR(32)  NOT NULL CHECK (approver_type IN ('ROLE','EMPLOYEE')),
    approver_ref  BIGINT       NOT NULL,
    -- ANY: one approval clears the node. ALL: every assignee must approve.
    approve_mode  VARCHAR(32)  NOT NULL DEFAULT 'ANY' CHECK (approve_mode IN ('ANY','ALL')),
    UNIQUE (definition_id, seq)
);

CREATE TABLE approval_instances (
    id             BIGSERIAL    PRIMARY KEY,
    tenant_id      BIGINT       NOT NULL DEFAULT 1,
    definition_id  BIGINT       NOT NULL REFERENCES approval_definitions(id),
    biz_type       VARCHAR(50)  NOT NULL,
    biz_id         BIGINT       NOT NULL,
    biz_no         VARCHAR(100) NOT NULL,
    -- Denormalized display data (customer, amount, ...) so a todo list never
    -- calls back into the business service.
    biz_summary    JSONB        NOT NULL DEFAULT '{}'::jsonb,
    submitter_id   BIGINT       NOT NULL,
    submitter_name VARCHAR(100) NOT NULL DEFAULT '',
    -- RETURNED is terminal like REJECTED: the document goes back for edits
    -- and a resubmission starts a new instance.
    status         VARCHAR(32)  NOT NULL DEFAULT 'RUNNING'
                   CHECK (status IN ('RUNNING','APPROVED','REJECTED','RETURNED','CANCELLED')),
    current_seq    INT          NOT NULL DEFAULT 1,
    submitted_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    finished_at    TIMESTAMPTZ
);
CREATE INDEX approval_instances_biz_idx ON approval_instances (tenant_id, biz_type, biz_id);
-- One document can only have one flow in flight; history stays queryable.
CREATE UNIQUE INDEX approval_instances_running_idx
    ON approval_instances (tenant_id, biz_type, biz_id) WHERE status = 'RUNNING';

CREATE TABLE approval_tasks (
    id          BIGSERIAL    PRIMARY KEY,
    tenant_id   BIGINT       NOT NULL DEFAULT 1,
    instance_id BIGINT       NOT NULL REFERENCES approval_instances(id) ON DELETE CASCADE,
    node_seq    INT          NOT NULL,
    node_name   VARCHAR(100) NOT NULL,
    assignee_id BIGINT       NOT NULL,
    status      VARCHAR(32)  NOT NULL DEFAULT 'PENDING'
                CHECK (status IN ('PENDING','APPROVED','REJECTED','RETURNED','SKIPPED','CANCELLED')),
    comment     TEXT         NOT NULL DEFAULT '',
    acted_at    TIMESTAMPTZ,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);
-- The index behind "我的待办".
CREATE INDEX approval_tasks_todo_idx
    ON approval_tasks (tenant_id, assignee_id) WHERE status = 'PENDING';
CREATE INDEX approval_tasks_instance_idx ON approval_tasks (instance_id, node_seq);

CREATE TABLE outbox_events (
    id             BIGSERIAL PRIMARY KEY,
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
    ON outbox_events (created_at) WHERE published_at IS NULL;

CREATE TABLE processed_events (
    event_id       VARCHAR(200) NOT NULL,
    consumer_group VARCHAR(100) NOT NULL,
    processed_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, consumer_group)
);

-- A starter flow so contracts have something to run through. Both nodes point
-- at role 1 (SUPER_ADMIN) because that is the only role iam bootstraps; once a
-- company creates 销售主管 / 总经理 roles it repoints approver_ref, which is
-- data, not code.
INSERT INTO approval_definitions (tenant_id, biz_type, name, version) VALUES
  (1, 'CONTRACT', '出口合同审批', 1);

INSERT INTO approval_nodes (tenant_id, definition_id, seq, name, approver_type, approver_ref, approve_mode)
SELECT 1, d.id, 1, '销售主管审批', 'ROLE', 1, 'ANY' FROM approval_definitions d WHERE d.biz_type = 'CONTRACT'
UNION ALL
SELECT 1, d.id, 2, '总经理审批', 'ROLE', 1, 'ANY' FROM approval_definitions d WHERE d.biz_type = 'CONTRACT';

-- +goose Down
DROP TABLE processed_events;
DROP TABLE outbox_events;
DROP TABLE approval_tasks;
DROP TABLE approval_instances;
DROP TABLE approval_nodes;
DROP TABLE approval_definitions;

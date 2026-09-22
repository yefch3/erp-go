-- +goose Up
ALTER TABLE contracts DROP CONSTRAINT contracts_status_check;
ALTER TABLE contracts ADD CONSTRAINT contracts_status_check CHECK (status IN
 ('DRAFT','PENDING_APPROVAL','PENDING_SIGN','REJECTED','EFFECTIVE','EXECUTING','COMPLETED','CANCELLED','PAUSED','TERMINATING','TERMINATED','DELETED'));
CREATE UNIQUE INDEX contracts_tenant_identity_idx ON contracts(tenant_id,id);
CREATE TABLE contract_workflows (
 tenant_id bigint NOT NULL CHECK (tenant_id > 0), contract_id bigint NOT NULL,
 history jsonb NOT NULL DEFAULT '{}',
 revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0), previous_status text NOT NULL DEFAULT '',
 termination_request_key text NOT NULL DEFAULT '', termination_instance_id bigint NOT NULL DEFAULT 0,
 termination_reason text NOT NULL DEFAULT '',
 PRIMARY KEY(tenant_id,contract_id),
 FOREIGN KEY(tenant_id,contract_id) REFERENCES contracts(tenant_id,id)
);
CREATE TABLE contract_workflow_actions (
 id bigserial PRIMARY KEY, tenant_id bigint NOT NULL CHECK (tenant_id > 0), contract_id bigint NOT NULL,
 action text NOT NULL, reason text NOT NULL DEFAULT '', data jsonb NOT NULL DEFAULT '{}',
 actor_id bigint NOT NULL, actor_name text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(tenant_id,contract_id) REFERENCES contracts(tenant_id,id)
);
CREATE TABLE contract_history_drafts (
 id bigserial PRIMARY KEY, tenant_id bigint NOT NULL CHECK (tenant_id > 0), owner_id bigint NOT NULL CHECK (owner_id > 0),
 body jsonb NOT NULL, revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0), contract_id bigint,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(tenant_id,contract_id) REFERENCES contracts(tenant_id,id)
);

CREATE INDEX contract_workflow_actions_lookup_idx ON contract_workflow_actions(tenant_id,contract_id,id DESC);
CREATE INDEX contract_history_drafts_owner_idx ON contract_history_drafts(tenant_id,owner_id,updated_at DESC) WHERE contract_id IS NULL;

-- +goose Down
DROP TABLE contract_history_drafts;
DROP TABLE contract_workflow_actions;
DROP TABLE contract_workflows;
DROP INDEX contracts_tenant_identity_idx;
-- Status values are retained to avoid rewriting completed audit history.

-- +goose Up
CREATE TABLE contract_handoff_plans (
 tenant_id bigint NOT NULL, contract_version_id bigint NOT NULL REFERENCES contract_versions(id),
 revision bigint NOT NULL DEFAULT 1, batches jsonb NOT NULL DEFAULT '[]',
 updated_by bigint NOT NULL, updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,contract_version_id)
);
-- +goose Down
DROP TABLE contract_handoff_plans;

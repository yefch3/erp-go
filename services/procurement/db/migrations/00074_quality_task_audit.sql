-- +goose Up
-- No foreign key to the task: the audit survives deletion and recreation.
CREATE TABLE quality_task_audit (
 id BIGSERIAL PRIMARY KEY,
 tenant_id BIGINT NOT NULL,
 task_id BIGINT NOT NULL,
 action VARCHAR(20) NOT NULL CHECK (action IN ('UPDATE_BASICS','DELETE')),
 actor_id BIGINT NOT NULL,
 actor_name TEXT NOT NULL,
 snapshot JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX quality_task_audit_task_idx ON quality_task_audit(tenant_id,task_id,created_at);
-- +goose Down
DROP TABLE quality_task_audit;

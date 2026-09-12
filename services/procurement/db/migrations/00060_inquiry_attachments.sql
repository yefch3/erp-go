-- +goose Up
CREATE TABLE inquiry_attachments (
 tenant_id bigint NOT NULL,case_id bigint NOT NULL REFERENCES sourcing_cases(id),
 revision bigint NOT NULL,kind text NOT NULL,object_key text NOT NULL,
 name text NOT NULL,created_by bigint NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,object_key)
);
-- +goose Down
DROP TABLE inquiry_attachments;

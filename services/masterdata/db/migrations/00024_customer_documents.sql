-- +goose Up
CREATE TABLE customer_documents (
 id BIGSERIAL PRIMARY KEY,
 tenant_id BIGINT NOT NULL,
 customer_id BIGINT NOT NULL,
 document_id BIGINT NOT NULL,
 version INT NOT NULL CHECK (version > 0),
 title TEXT NOT NULL,
 remark TEXT NOT NULL DEFAULT '',
 file_key TEXT NOT NULL,
 file_name TEXT NOT NULL,
 content_type TEXT NOT NULL,
 size_bytes BIGINT NOT NULL CHECK (size_bytes > 0 AND size_bytes <= 10485760),
 expires_on DATE,
 remind_days INT NOT NULL DEFAULT 30 CHECK (remind_days BETWEEN 0 AND 3650),
 reminder_enabled BOOLEAN NOT NULL DEFAULT true,
 uploaded_by BIGINT NOT NULL,
 uploaded_by_name TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(tenant_id, document_id, version),
 UNIQUE(tenant_id, id)
);
CREATE INDEX customer_documents_customer_idx ON customer_documents(tenant_id, customer_id, document_id, version DESC);
CREATE TABLE customer_document_reads (
 tenant_id BIGINT NOT NULL,
 revision_id BIGINT NOT NULL,
 employee_id BIGINT NOT NULL,
 read_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id, revision_id, employee_id),
 FOREIGN KEY(tenant_id, revision_id) REFERENCES customer_documents(tenant_id,id)
);
-- +goose Down
DROP TABLE customer_document_reads;
DROP TABLE customer_documents;

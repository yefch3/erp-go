-- +goose Up
CREATE TABLE supplier_documents (
 id BIGSERIAL PRIMARY KEY,
 tenant_id BIGINT NOT NULL CHECK (tenant_id > 0),
 supplier_id BIGINT NOT NULL,
 document_id BIGINT NOT NULL,
 version INT NOT NULL CHECK (version > 0),
 title TEXT NOT NULL,
 remark TEXT NOT NULL DEFAULT '',
 file_key TEXT NOT NULL,
 file_name TEXT NOT NULL,
 content_type TEXT NOT NULL,
 size_bytes BIGINT NOT NULL CHECK (size_bytes > 0 AND size_bytes <= 10485760),
 expires_on DATE,
 remind_days INT NOT NULL DEFAULT 0 CHECK (remind_days BETWEEN 0 AND 3650),
 reminder_enabled BOOLEAN NOT NULL DEFAULT true,
 uploaded_by BIGINT NOT NULL,
 uploaded_by_name TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 deleted_at TIMESTAMPTZ,
 UNIQUE(tenant_id, document_id, version),
 UNIQUE(tenant_id, id),
 FOREIGN KEY(supplier_id) REFERENCES suppliers(id) ON DELETE RESTRICT
);
CREATE INDEX supplier_documents_supplier_idx ON supplier_documents(tenant_id, supplier_id, document_id, version DESC);

CREATE TABLE supplier_document_reads (
 tenant_id BIGINT NOT NULL CHECK (tenant_id > 0),
 revision_id BIGINT NOT NULL,
 employee_id BIGINT NOT NULL,
 read_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id, revision_id, employee_id),
 FOREIGN KEY(tenant_id, revision_id) REFERENCES supplier_documents(tenant_id,id)
);

-- +goose Down
DROP TABLE supplier_document_reads;
DROP TABLE supplier_documents;

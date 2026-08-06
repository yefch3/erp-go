-- +goose Up

CREATE TABLE shipping_documents (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL DEFAULT 1,
    schedule_id BIGINT NOT NULL REFERENCES shipping_schedules(id),
    document_group_key VARCHAR(32) NOT NULL,
    category VARCHAR(32) NOT NULL CHECK (category IN (
        'EXPORT_CONTRACT', 'BOOKING_CONFIRMATION', 'COMMERCIAL_INVOICE',
        'PACKING_LIST', 'CUSTOMS_DOCUMENT', 'BILL_OF_LADING_DRAFT',
        'BILL_OF_LADING_FINAL', 'ARRIVAL_NOTICE', 'OTHER'
    )),
    version INT NOT NULL CHECK (version > 0),
    file_name VARCHAR(255) NOT NULL,
    file_key VARCHAR(700) NOT NULL,
    file_size BIGINT NOT NULL CHECK (file_size > 0 AND file_size <= 20971520),
    content_type VARCHAR(150) NOT NULL DEFAULT 'application/octet-stream',
    remark TEXT NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'VOIDED')),
    uploaded_by BIGINT NOT NULL,
    uploaded_by_name VARCHAR(100) NOT NULL DEFAULT '',
    uploaded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    voided_by BIGINT,
    voided_by_name VARCHAR(100),
    voided_at TIMESTAMPTZ,
    void_reason TEXT,
    UNIQUE (tenant_id, schedule_id, document_group_key, version),
    UNIQUE (tenant_id, file_key)
);

CREATE INDEX shipping_documents_schedule_idx
    ON shipping_documents (tenant_id, schedule_id, uploaded_at DESC);
CREATE INDEX shipping_documents_group_idx
    ON shipping_documents (tenant_id, schedule_id, document_group_key, version DESC);

ALTER TABLE shipping_schedule_changes
    DROP CONSTRAINT shipping_schedule_changes_change_type_check;
ALTER TABLE shipping_schedule_changes
    ADD CONSTRAINT shipping_schedule_changes_change_type_check
        CHECK (change_type IN ('DATE','ETA','STATUS','CANCEL','VESSEL_VOYAGE','ROUTE','TEMPORARY_CALL','PROGRESS','DELAY','DOCUMENT'));

-- +goose Down

DELETE FROM shipping_schedule_changes WHERE change_type = 'DOCUMENT';
ALTER TABLE shipping_schedule_changes
    DROP CONSTRAINT shipping_schedule_changes_change_type_check;
ALTER TABLE shipping_schedule_changes
    ADD CONSTRAINT shipping_schedule_changes_change_type_check
        CHECK (change_type IN ('DATE','ETA','STATUS','CANCEL','VESSEL_VOYAGE','ROUTE','TEMPORARY_CALL','PROGRESS','DELAY'));
DROP TABLE shipping_documents;

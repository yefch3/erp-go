-- +goose Up
-- D5 factory pre-shipment inspections. These tables deliberately do not
-- reference purchase_receipts: the older inspection record is an after-
-- receipt warehouse record, while this workflow starts at the factory.
CREATE TABLE quality_inspection_tasks (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    po_id BIGINT NOT NULL REFERENCES purchase_orders(id),
    task_no VARCHAR(64) NOT NULL,
    batch_no INTEGER NOT NULL CHECK (batch_no > 0),
    status VARCHAR(20) NOT NULL DEFAULT 'WAITING'
      CHECK (status IN ('WAITING','IN_PROGRESS','REINSPECTION','COMPLETED')),
    expected_date DATE,
    inspection_location TEXT NOT NULL DEFAULT '',
    contact_name VARCHAR(100) NOT NULL DEFAULT '',
    contact_phone VARCHAR(64) NOT NULL DEFAULT '',
    remark TEXT NOT NULL DEFAULT '',
    requested_by BIGINT NOT NULL,
    requested_by_name VARCHAR(100) NOT NULL DEFAULT '',
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    inspector_id BIGINT NOT NULL DEFAULT 0,
    inspector_name VARCHAR(100) NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, task_no), UNIQUE (tenant_id, po_id, batch_no)
);
CREATE INDEX quality_tasks_queue_idx ON quality_inspection_tasks
  (tenant_id, status, requested_at DESC);

CREATE TABLE quality_inspection_task_lines (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    task_id BIGINT NOT NULL REFERENCES quality_inspection_tasks(id) ON DELETE CASCADE,
    po_item_id BIGINT NOT NULL REFERENCES purchase_order_items(id),
    product_name VARCHAR(200) NOT NULL DEFAULT '',
    spec VARCHAR(300) NOT NULL DEFAULT '',
    uom_code VARCHAR(32) NOT NULL DEFAULT '',
    ordered_qty NUMERIC(18,4) NOT NULL CHECK (ordered_qty > 0),
    requested_qty NUMERIC(18,4) NOT NULL CHECK (requested_qty > 0),
    qualified_qty NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (qualified_qty >= 0),
    unresolved_qty NUMERIC(18,4) NOT NULL CHECK (unresolved_qty >= 0),
    final_result VARCHAR(16) NOT NULL DEFAULT 'PENDING'
      CHECK (final_result IN ('PENDING','PASS','PARTIAL','FAIL')),
    issue_description TEXT NOT NULL DEFAULT '',
    handling_suggestion TEXT NOT NULL DEFAULT '',
    approved_release_qty NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (approved_release_qty >= 0),
    release_decided_by BIGINT NOT NULL DEFAULT 0,
    release_decided_by_name VARCHAR(100) NOT NULL DEFAULT '',
    release_decided_at TIMESTAMPTZ,
    UNIQUE (tenant_id, task_id, po_item_id),
    CHECK (qualified_qty + unresolved_qty = requested_qty),
    CHECK (approved_release_qty <= qualified_qty)
);

CREATE TABLE quality_inspection_rounds (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    task_id BIGINT NOT NULL REFERENCES quality_inspection_tasks(id) ON DELETE CASCADE,
    round_no INTEGER NOT NULL CHECK (round_no > 0),
    inspected_at TIMESTAMPTZ NOT NULL,
    inspection_location TEXT NOT NULL DEFAULT '',
    inspector_id BIGINT NOT NULL,
    inspector_name VARCHAR(100) NOT NULL DEFAULT '',
    remark TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, task_id, round_no)
);

CREATE TABLE quality_inspection_round_lines (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    round_id BIGINT NOT NULL REFERENCES quality_inspection_rounds(id) ON DELETE CASCADE,
    task_line_id BIGINT NOT NULL REFERENCES quality_inspection_task_lines(id),
    result VARCHAR(16) NOT NULL CHECK (result IN ('PASS','PARTIAL','FAIL')),
    inspected_qty NUMERIC(18,4) NOT NULL CHECK (inspected_qty > 0),
    qualified_qty NUMERIC(18,4) NOT NULL CHECK (qualified_qty >= 0),
    unqualified_qty NUMERIC(18,4) NOT NULL CHECK (unqualified_qty >= 0),
    issue_description TEXT NOT NULL DEFAULT '',
    handling_suggestion TEXT NOT NULL DEFAULT '',
    CHECK (qualified_qty + unqualified_qty = inspected_qty),
    UNIQUE (tenant_id, round_id, task_line_id)
);

CREATE TABLE quality_inspection_files (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    task_id BIGINT NOT NULL REFERENCES quality_inspection_tasks(id) ON DELETE CASCADE,
    round_id BIGINT REFERENCES quality_inspection_rounds(id) ON DELETE CASCADE,
    task_line_id BIGINT REFERENCES quality_inspection_task_lines(id),
    category VARCHAR(20) NOT NULL
      CHECK (category IN ('PHOTO','VIDEO','REPORT','THIRD_PARTY','OTHER')),
    object_key TEXT NOT NULL,
    file_name TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT '',
    size_bytes BIGINT NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    supplemental BOOLEAN NOT NULL DEFAULT false,
    uploaded_by BIGINT NOT NULL,
    uploaded_by_name VARCHAR(100) NOT NULL DEFAULT '',
    uploaded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, object_key)
);

-- +goose Down
DROP TABLE quality_inspection_files;
DROP TABLE quality_inspection_round_lines;
DROP TABLE quality_inspection_rounds;
DROP TABLE quality_inspection_task_lines;
DROP TABLE quality_inspection_tasks;

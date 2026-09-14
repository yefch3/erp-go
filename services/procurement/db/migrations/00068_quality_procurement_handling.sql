-- +goose Up
ALTER TABLE quality_inspection_tasks
  ADD COLUMN procurement_handling_status VARCHAR(16) NOT NULL DEFAULT 'NONE'
    CHECK (procurement_handling_status IN ('NONE','PENDING','COMPLETED')),
  ADD COLUMN procurement_handling_action VARCHAR(24) NOT NULL DEFAULT ''
    CHECK (procurement_handling_action IN ('','REWORK','REPLACEMENT','CANCEL_SHORTAGE')),
  ADD COLUMN procurement_handling_note TEXT NOT NULL DEFAULT '',
  ADD COLUMN procurement_handled_by BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN procurement_handled_by_name VARCHAR(100) NOT NULL DEFAULT '',
  ADD COLUMN procurement_handled_at TIMESTAMPTZ;

CREATE INDEX quality_procurement_todo_idx ON quality_inspection_tasks
  (tenant_id, procurement_handling_status, updated_at DESC);

-- Retain the old columns for migration compatibility, but clear every partial
-- release. Only a task whose unresolved quantity is zero can be shipped.
UPDATE quality_inspection_task_lines
SET approved_release_qty = CASE WHEN unresolved_qty = 0 THEN qualified_qty ELSE 0 END,
    release_decided_by = 0, release_decided_by_name = '', release_decided_at = NULL;

-- +goose Down
DROP INDEX quality_procurement_todo_idx;
ALTER TABLE quality_inspection_tasks
  DROP COLUMN procurement_handled_at,
  DROP COLUMN procurement_handled_by_name,
  DROP COLUMN procurement_handled_by,
  DROP COLUMN procurement_handling_note,
  DROP COLUMN procurement_handling_action,
  DROP COLUMN procurement_handling_status;

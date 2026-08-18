-- name: CreateExcelJob :one
INSERT INTO mail_excel_jobs (
  tenant_id, owner_id, inbound_id, attachment_id, selected_text, locale, template_columns
) VALUES (
  sqlc.arg(tenant_id), sqlc.arg(owner_id), sqlc.arg(inbound_id),
  sqlc.narg(attachment_id), sqlc.narg(selected_text), sqlc.arg(locale),
  sqlc.arg(template_columns)::jsonb
)
RETURNING *;

-- name: GetExcelJob :one
SELECT * FROM mail_excel_jobs
WHERE tenant_id=sqlc.arg(tenant_id) AND owner_id=sqlc.arg(owner_id) AND id=sqlc.arg(id);

-- name: ClaimExcelJob :one
WITH candidate AS (
  SELECT id
  FROM mail_excel_jobs
  WHERE status='PENDING'
     OR (status='PROCESSING' AND started_at < now() - interval '15 minutes')
  ORDER BY created_at, id
  FOR UPDATE SKIP LOCKED
  LIMIT 1
)
UPDATE mail_excel_jobs j SET
  status='PROCESSING', attempt_count=attempt_count+1,
  started_at=now(), completed_at=NULL,
  error_code='', error_message='', updated_at=now()
FROM candidate
WHERE j.id=candidate.id
RETURNING j.*;

-- name: CompleteExcelJob :execrows
UPDATE mail_excel_jobs SET
  status='COMPLETED', file_name=sqlc.arg(file_name), file_data=sqlc.arg(file_data),
  workbook_json=sqlc.arg(workbook_json), model=sqlc.arg(model),
  error_code='', error_message='', completed_at=now(), updated_at=now()
WHERE id=sqlc.arg(id) AND status='PROCESSING';

-- name: FailExcelJob :execrows
UPDATE mail_excel_jobs SET
  status='FAILED', file_data=NULL, workbook_json=NULL,
  error_code=sqlc.arg(error_code), error_message=sqlc.arg(error_message),
  completed_at=now(), updated_at=now()
WHERE id=sqlc.arg(id) AND status='PROCESSING';

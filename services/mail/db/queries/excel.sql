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

-- name: RecordExcelJobUsage :execrows
-- 累加这次调用花掉的 token。重试会走到这里几遍，每遍都真花了钱。
--
-- 和成功/失败分开写：一次任务可能先失败几次再成功，用量要全算上，而
-- CompleteExcelJob / FailExcelJob 只该管状态。
UPDATE mail_excel_jobs SET
  input_tokens  = input_tokens  + sqlc.arg(input_tokens)::bigint,
  output_tokens = output_tokens + sqlc.arg(output_tokens)::bigint,
  updated_at = now()
WHERE id = sqlc.arg(id)::bigint;

-- name: ExcelUsageByMonth :many
-- 智能转换的用量账：一个月一行，按人拆开。
--
-- jobs 表本身就是账本，不另建汇总表——这套东西一个月几十到几百次，为它
-- 维护一张会和账本失同步的汇总表是提前优化。
--
-- 只出 token 数，不出金额：金额由读的一方按当下单价算。单价会因为谈折扣、
-- 换模型而变，存进去等于把一个会过期的判断固化成历史。
SELECT
    to_char(date_trunc('month', created_at), 'YYYY-MM')::text AS month,
    owner_id,
    count(*)::bigint                                            AS runs,
    count(*) FILTER (WHERE status = 'COMPLETED')::bigint        AS succeeded,
    count(*) FILTER (WHERE status = 'FAILED')::bigint           AS failed,
    coalesce(sum(input_tokens), 0)::bigint                      AS input_tokens,
    coalesce(sum(output_tokens), 0)::bigint                     AS output_tokens
FROM mail_excel_jobs
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND (sqlc.arg(month)::text = ''
       OR to_char(date_trunc('month', created_at), 'YYYY-MM') = sqlc.arg(month)::text)
GROUP BY 1, 2
ORDER BY 1 DESC, runs DESC;

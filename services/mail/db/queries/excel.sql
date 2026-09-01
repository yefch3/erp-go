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

-- name: CurrentUsageMonth :one
-- 「这个月」是哪个月，由数据库说了算。
--
-- 不在 Go 里算 time.Now()：那是两个时钟、两个时区。只要容器和数据库对月
-- 份的理解差一点点，就会出现页面显示「41 次」而拦截说「已用 42 次」这种
-- 谁也解释不清的事——而且只在每月月初那几个小时出现，最难查。
SELECT to_char(date_trunc('month', now()), 'YYYY-MM')::text;

-- name: CountExcelRunsThisMonth :one
-- **这个人**这个月转了多少次——一个数，给额度用。
--
-- 2026-09-01 从「这家公司」改成「这个人」：额度的单位是每人每月，不是全公司
-- 每月。改之前一个人跑几十次就把同事全挡在外面，而挡人的那句话说的是「本月
-- 智能转换额度已用完」——被挡的人根本不知道额度被谁用掉了。
--
-- 写成半开区间而不是 to_char(...) = '2026-08'：前者能走 (tenant_id,
-- created_at) 索引的范围扫描，后者要对每一行算一次函数。
SELECT count(*)::bigint
FROM mail_excel_jobs
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND created_at >= date_trunc('month', now())
  AND created_at <  date_trunc('month', now()) + interval '1 month';

-- name: ExcelRunsByTenantThisMonth :many
-- 每家公司这个月各转了多少次、烧了多少 token。只有平台运营看得到这一条
-- ——它跨租户。
--
-- token 一起出，是因为「多少次」是给客户看的额度口径，「多少钱」是给我们
-- 看的成本口径，而这两个数只有并排放着才看得出问题：有的公司次数不多但每
-- 次都是几十兆的附件。金额仍然在读的时候按当下单价算，不落库。
SELECT
    tenant_id,
    count(*)::bigint                       AS runs,
    coalesce(sum(input_tokens), 0)::bigint  AS input_tokens,
    coalesce(sum(output_tokens), 0)::bigint AS output_tokens
FROM mail_excel_jobs
WHERE created_at >= date_trunc('month', now())
  AND created_at <  date_trunc('month', now()) + interval '1 month'
GROUP BY tenant_id;

-- name: GetExcelQuota :one
SELECT * FROM mail_excel_quotas WHERE tenant_id = sqlc.arg(tenant_id)::bigint;

-- name: ListExcelQuotas :many
-- 跨租户，平台运营专用。
SELECT * FROM mail_excel_quotas ORDER BY tenant_id;

-- name: SetExcelQuota :exec
INSERT INTO mail_excel_quotas (tenant_id, monthly_runs, updated_by, updated_at)
VALUES (sqlc.arg(tenant_id)::bigint, sqlc.arg(monthly_runs)::bigint, sqlc.arg(updated_by)::bigint, now())
ON CONFLICT (tenant_id) DO UPDATE SET
  monthly_runs = EXCLUDED.monthly_runs,
  updated_by   = EXCLUDED.updated_by,
  updated_at   = now();

-- name: ClearExcelQuota :exec
-- 删掉这一行就是恢复不限。不是把 monthly_runs 改成 0——0 是「一次都不许
-- 用」，和「不限」正好相反。
DELETE FROM mail_excel_quotas WHERE tenant_id = sqlc.arg(tenant_id)::bigint;

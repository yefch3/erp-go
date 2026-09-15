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
-- 完成一次转换。**只写库，一条语句落地**——状态、workbook、字节一起。
--
-- 不在这里传对象存储：那是第二套系统，两次写之间没有事务，而这一刻正是最不
-- 能出"一半"的时候（模型刚花完钱）。字节先落在 file_data 里，upload_next_try_at
-- 置为现在就等于排进了搬运队列，剩下的交给搬运工重试到成功（见迁移 00066）。
UPDATE mail_excel_jobs SET
  status='COMPLETED', file_name=sqlc.arg(file_name), file_data=sqlc.arg(file_data),
  workbook_json=sqlc.arg(workbook_json), model=sqlc.arg(model),
  file_key='', upload_attempts=0, upload_next_try_at=now(), upload_last_error='',
  error_code='', error_message='', completed_at=now(), updated_at=now()
WHERE id=sqlc.arg(id) AND status='PROCESSING';

-- name: ClaimExcelUpload :many
-- 搬运工要搬的那一批：字节还在库里、还没搬上去、到点可以再试的。
--
-- 不用 FOR UPDATE SKIP LOCKED：搬这件事是幂等的（键是确定的，重传就是覆盖），
-- 两个副本同时搬同一份的后果只是多传一次，而锁的代价是一直持着事务在传文件。
SELECT id, tenant_id, file_name, file_data, upload_attempts
FROM mail_excel_jobs
WHERE file_data IS NOT NULL AND file_key = ''
  AND (upload_next_try_at IS NULL OR upload_next_try_at <= now())
ORDER BY upload_next_try_at, id
LIMIT sqlc.arg(row_limit)::int;

-- name: MarkExcelUploaded :execrows
-- 传上去了：记下 key，同一条语句里把字节清掉。
--
-- **必须是同一条。** 分两条的话中间那一刻两处都有，而更糟的是先清字节、
-- 后写 key 失败——那时两处都没有，人拿不到文件。
UPDATE mail_excel_jobs
SET file_key=sqlc.arg(file_key), file_data=NULL,
    upload_last_error='', upload_next_try_at=NULL, updated_at=now()
WHERE id=sqlc.arg(id)::bigint AND file_key='';

-- name: DelayExcelUpload :execrows
-- 这一趟没传上去：记一笔，退避之后再来。**字节一个都不动**——它现在是人
-- 唯一能取到这份文件的地方。
UPDATE mail_excel_jobs
SET upload_attempts=upload_attempts+1,
    upload_last_error=sqlc.arg(last_error),
    upload_next_try_at=now() + sqlc.arg(retry_in)::interval,
    updated_at=now()
WHERE id=sqlc.arg(id)::bigint;

-- name: FailExcelJob :execrows
UPDATE mail_excel_jobs SET
  status='FAILED', file_key='', file_data=NULL, workbook_json=NULL,
  upload_next_try_at=NULL,
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

-- name: ListExpiredExcelPayloads :many
-- 过了窗口期、还没收过的任务。清理器拿这一批，先删对象存储里那一份，再清行。
--
-- **判据是 payload_cleared_at，不是「列里还有没有东西」。** 后者听起来更直接，
-- 但它恰好漏掉最该收的那一种：对象写成功、行写失败之后留下的**孤儿**——
-- 那一行的 file_key 和 file_data 都是空的，而对象还在桶里躺着。
--
-- 所以这里不挑，凡是结束了又没收过的都拿出来，让 Go 按「租户/任务号」算出
-- 对象键去删一次。算得出来是因为那个键本来就不依赖任何存下来的字段，而 S3
-- 的 DELETE 对不存在的键是幂等的——没有对象的那些，这一下什么都不会发生。
--
-- 分两步而不是一条 UPDATE：对象得由 Go 去删，而且**一个删不掉不该连累
-- 一整批**——所以逐行处理，删不掉的那一行不清，下一趟再试。
SELECT id, tenant_id
FROM mail_excel_jobs
WHERE completed_at IS NOT NULL
  AND completed_at < sqlc.arg(cutoff)::timestamptz
  AND payload_cleared_at IS NULL
ORDER BY id
LIMIT sqlc.arg(row_limit)::int;

-- name: ClearExcelJobPayload :execrows
-- 清掉这一行的结果，**行留着**。
--
-- 行不能删：用量账（ExcelUsageByMonth）数的就是这张表的行，按月按人统计跑了
-- 几次、花了多少 token。它要的几列（created_at / owner_id / status / *_tokens）
-- 一个字节的文件内容都不需要。删行等于把账烧了。
--
-- 谁都够不着了才清：任务号只活在浏览器的 sessionStorage 里（标签页一关就没），
-- 而且没有任何界面列得出历史任务——这个文件里也没有对应的查询。
UPDATE mail_excel_jobs
SET file_key='', file_data=NULL, workbook_json=NULL,
    upload_next_try_at=NULL, payload_cleared_at=now(), updated_at=now()
WHERE id=sqlc.arg(id)::bigint;

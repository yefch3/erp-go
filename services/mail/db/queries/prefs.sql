-- name: GetMailListMode :one
-- 这个人的列表模式。没有行 = 没改过 = 'THREAD'，所以读取那一侧永远不需要
-- 先插一行；调用方把 pgx.ErrNoRows 当成默认档处理（见 app/prefs.go）。
SELECT list_mode
FROM mail_list_prefs
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND employee_id = sqlc.arg(employee_id)::bigint;

-- name: SetMailListMode :exec
-- 改档。第一次改才插行——这也是为什么读取要能接受「没有行」。
INSERT INTO mail_list_prefs (tenant_id, employee_id, list_mode, updated_at)
VALUES (sqlc.arg(tenant_id)::bigint, sqlc.arg(employee_id)::bigint,
        sqlc.arg(list_mode)::text, now())
ON CONFLICT (tenant_id, employee_id) DO UPDATE
    SET list_mode = EXCLUDED.list_mode,
        updated_at = now();

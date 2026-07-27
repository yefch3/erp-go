-- name: ActiveDefinitionFor :one
SELECT * FROM approval_definitions
WHERE tenant_id = $1 AND biz_type = $2 AND status = 'ACTIVE'
ORDER BY version DESC
LIMIT 1;

-- name: ListNodes :many
SELECT * FROM approval_nodes
WHERE tenant_id = $1 AND definition_id = $2
ORDER BY seq;

-- name: GetNode :one
SELECT * FROM approval_nodes
WHERE tenant_id = $1 AND definition_id = $2 AND seq = $3;

-- name: CreateInstance :one
INSERT INTO approval_instances (
    tenant_id, definition_id, biz_type, biz_id, biz_no, biz_summary,
    submitter_id, submitter_name
) VALUES ($1, $2, $3, $4, $5, sqlc.arg(biz_summary)::jsonb, $6, $7)
RETURNING *;

-- name: GetInstance :one
SELECT * FROM approval_instances WHERE tenant_id = $1 AND id = $2;

-- name: GetInstanceForUpdate :one
SELECT * FROM approval_instances WHERE tenant_id = $1 AND id = $2 FOR UPDATE;

-- name: ListInstancesByBiz :many
SELECT * FROM approval_instances
WHERE tenant_id = $1 AND biz_type = $2 AND biz_id = $3
ORDER BY submitted_at DESC;

-- name: AdvanceInstance :one
UPDATE approval_instances SET current_seq = $3
WHERE tenant_id = $1 AND id = $2
RETURNING *;

-- name: FinishInstance :one
UPDATE approval_instances SET status = $3, finished_at = now()
WHERE tenant_id = $1 AND id = $2
RETURNING *;

-- name: CreateTask :one
INSERT INTO approval_tasks (tenant_id, instance_id, node_seq, node_name, assignee_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetTaskForUpdate :one
SELECT * FROM approval_tasks WHERE tenant_id = $1 AND id = $2 FOR UPDATE;

-- name: ActOnTask :one
UPDATE approval_tasks SET status = $3, comment = $4, acted_at = now()
WHERE tenant_id = $1 AND id = $2 AND status = 'PENDING'
RETURNING *;

-- name: CloseSiblingTasks :exec
UPDATE approval_tasks SET status = $4, acted_at = now()
WHERE tenant_id = $1 AND instance_id = $2 AND node_seq = $3 AND status = 'PENDING';

-- name: CancelInstanceTasks :exec
UPDATE approval_tasks SET status = 'CANCELLED', acted_at = now()
WHERE tenant_id = $1 AND instance_id = $2 AND status = 'PENDING';

-- name: CountPendingAtNode :one
SELECT count(*) FROM approval_tasks
WHERE tenant_id = $1 AND instance_id = $2 AND node_seq = $3 AND status = 'PENDING';

-- name: ListTasksByInstance :many
SELECT * FROM approval_tasks
WHERE tenant_id = $1 AND instance_id = $2
ORDER BY node_seq, id;

-- name: ListMyTasks :many
-- One query serves every tab of "my approvals": the pending queue by
-- default, everything already handled, or one exact decision.
SELECT
    t.id, t.instance_id, t.node_seq, t.node_name, t.assignee_id, t.status,
    t.comment, t.acted_at, t.created_at,
    i.biz_type, i.biz_id, i.biz_no, i.biz_summary, i.submitter_id,
    i.submitter_name, i.status AS instance_status, i.current_seq, i.submitted_at,
    count(*) OVER () AS total
FROM approval_tasks t
JOIN approval_instances i ON i.id = t.instance_id AND i.tenant_id = t.tenant_id
WHERE t.tenant_id = sqlc.arg(tenant_id)::bigint
  AND t.assignee_id = sqlc.arg(assignee_id)::bigint
  AND (sqlc.arg(biz_type)::text = '' OR i.biz_type = sqlc.arg(biz_type)::text)
  AND CASE
        WHEN sqlc.arg(status)::text = ''        THEN t.status = 'PENDING'
        WHEN sqlc.arg(status)::text = 'HANDLED' THEN t.status <> 'PENDING'
        ELSE t.status = sqlc.arg(status)::text
      END
-- Pending rows have no acted_at, so newest-first works for both tabs.
ORDER BY coalesce(t.acted_at, t.created_at) DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

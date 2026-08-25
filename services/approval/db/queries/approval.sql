-- name: ActiveDefinitionFor :one
-- The highest band floor at or below the amount. Because bands are defined by
-- their floor and the base band is 0, exactly one row always matches: gaps and
-- overlaps are not expressible.
SELECT * FROM approval_definitions
WHERE tenant_id = $1 AND biz_type = $2 AND status = 'ACTIVE'
  AND min_amount <= sqlc.arg(amount)::text::numeric
ORDER BY min_amount DESC, version DESC
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
    submitter_id, submitter_name, amount
) VALUES ($1, $2, $3, $4, $5, sqlc.arg(biz_summary)::jsonb, $6, $7,
    sqlc.arg(amount)::text::numeric)
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

-- name: CloseSiblingTasks :many
-- Returns whose queue just changed, so they can be told rather than left
-- staring at a task somebody else already handled.
UPDATE approval_tasks SET status = $4, acted_at = now()
WHERE tenant_id = $1 AND instance_id = $2 AND node_seq = $3 AND status = 'PENDING'
RETURNING assignee_id;

-- name: CancelInstanceTasks :many
UPDATE approval_tasks SET status = 'CANCELLED', acted_at = now()
WHERE tenant_id = $1 AND instance_id = $2 AND status = 'PENDING'
RETURNING assignee_id;

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

-- name: ListDefinitions :many
-- Every version ever, newest first per business type. Old ones are kept
-- because running instances still point at them.
SELECT d.id, d.biz_type, d.name, d.version, d.status, d.created_at,
       d.min_amount::text AS min_amount, d.created_by,
       (SELECT count(*) FROM approval_nodes n
        WHERE n.definition_id = d.id AND n.tenant_id = d.tenant_id)::int AS node_count,
       (SELECT count(*) FROM approval_instances i
        WHERE i.definition_id = d.id AND i.status = 'RUNNING')::int AS running_count
FROM approval_definitions d
WHERE d.tenant_id = $1
ORDER BY d.biz_type, d.version DESC;

-- name: GetDefinition :one
SELECT id, tenant_id, biz_type, name, version, status, created_at,
    min_amount::text AS min_amount, created_by
FROM approval_definitions WHERE tenant_id = $1 AND id = $2;

-- name: NextDefinitionVersion :one
-- Versions count per band, so editing the large-contract flow does not bump
-- the version number of the small-contract one.
SELECT coalesce(max(version), 0)::int + 1
FROM approval_definitions
WHERE tenant_id = $1 AND biz_type = $2
  AND min_amount = sqlc.arg(min_amount)::text::numeric;

-- name: CreateDefinition :one
INSERT INTO approval_definitions (
    tenant_id, biz_type, name, version, status, min_amount, created_by
) VALUES (
    $1, $2, $3, sqlc.arg(version)::int, 'ACTIVE',
    sqlc.arg(min_amount)::text::numeric, sqlc.arg(created_by)::bigint
)
RETURNING id, tenant_id, biz_type, name, version, status, created_at,
          min_amount::text AS min_amount, created_by;

-- name: DeactivateOtherDefinitions :exec
-- One active version per BAND; the previous one is retired rather than deleted
-- so instances that quote it still resolve. Other bands are left alone.
UPDATE approval_definitions SET status = 'INACTIVE'
WHERE tenant_id = $1 AND biz_type = $2
  AND min_amount = sqlc.arg(min_amount)::text::numeric
  AND id <> sqlc.arg(keep_id)::bigint AND status = 'ACTIVE';

-- name: DeleteBand :execrows
-- Retires every version of one band. The floor-0 band is never removable:
-- something has to match an amount of zero.
UPDATE approval_definitions SET status = 'INACTIVE'
WHERE tenant_id = $1 AND biz_type = $2
  AND min_amount = sqlc.arg(min_amount)::text::numeric
  AND min_amount > 0 AND status = 'ACTIVE';

-- name: CreateNode :one
INSERT INTO approval_nodes (tenant_id, definition_id, seq, name, approver_type, approver_ref, approve_mode)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: MyInvolvedBizIds :many
-- Documents this person has been asked to act on, whatever became of the
-- task. Business services union this with their own visibility rules: being
-- asked to approve something has to imply being allowed to read it.
SELECT DISTINCT i.biz_id
FROM approval_tasks t
JOIN approval_instances i ON i.id = t.instance_id AND i.tenant_id = t.tenant_id
WHERE t.tenant_id = $1 AND t.assignee_id = $2
  AND (sqlc.arg(biz_type)::text = '' OR i.biz_type = sqlc.arg(biz_type)::text);

-- 下面四条只服务「新公司开张时补一套默认审批流」，见 app/defaults.go。

-- name: CountDefinitionsFor :one
-- 这家公司在这个单据类型上到底有没有过审批流——任何状态都算。
--
-- 「一条都没有」和「有但都停用了」是两件事：前者是没播过种，后者是有人
-- 特意关掉的。只看 ACTIVE 会把后者也当成前者，于是每提交一次就把人家
-- 关掉的流程复活一次。
SELECT count(*)::bigint FROM approval_definitions
WHERE tenant_id = $1 AND biz_type = $2;

-- name: SeedDefinition :execrows
-- DO NOTHING 而不是覆盖：两个并发的提交同时走到这里，只有一个插得进去。
INSERT INTO approval_definitions (
    tenant_id, biz_type, name, version, status, min_amount, created_by
) VALUES (
    $1, $2, $3, 1, 'ACTIVE', sqlc.arg(min_amount)::text::numeric, 0
)
ON CONFLICT (tenant_id, biz_type, min_amount, version) DO NOTHING;

-- name: GetDefinitionBand :one
-- 播种后再读：并发时插入失败的那一方，读到的才是真正生效的那条。
SELECT id FROM approval_definitions
WHERE tenant_id = $1 AND biz_type = $2
  AND min_amount = sqlc.arg(min_amount)::text::numeric
  AND version = 1;

-- name: SeedNode :execrows
INSERT INTO approval_nodes (tenant_id, definition_id, seq, name, approver_type, approver_ref, approve_mode)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (definition_id, seq) DO NOTHING;

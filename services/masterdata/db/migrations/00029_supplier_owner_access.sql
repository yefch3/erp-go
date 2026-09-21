-- +goose Up
-- 旧供应商若还没有有效负责人，则把原创建人补为主负责人。
-- 每一处匹配都同时使用 tenant_id，避免不同公司间串数据。
INSERT INTO supplier_owners (
    tenant_id, supplier_id, employee_id, employee_name,
    responsibility_code, is_primary, created_by, updated_by
)
SELECT
    s.tenant_id,
    s.id,
    s.created_by,
    COALESCE(
        NULLIF((
            SELECT scl.operator_name
            FROM supplier_change_logs scl
            WHERE scl.tenant_id = s.tenant_id
              AND scl.supplier_id = s.id
              AND scl.operator_id = s.created_by
              AND btrim(scl.operator_name) <> ''
            ORDER BY scl.created_at, scl.id
            LIMIT 1
        ), ''),
        '员工 ' || s.created_by::text
    ),
    'PROCUREMENT',
    true,
    s.created_by,
    s.created_by
FROM suppliers s
WHERE s.created_by > 0
  AND NOT EXISTS (
      SELECT 1
      FROM supplier_owners so
      WHERE so.tenant_id = s.tenant_id
        AND so.supplier_id = s.id
        AND so.status = 'ACTIVE'
  )
ON CONFLICT DO NOTHING;

-- +goose Down
-- 无法可靠区分迁移补录与之后人工维护的负责人，回滚时保留权限关系。

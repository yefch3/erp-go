-- +goose Up
-- 客户负责人权限上线前，从邮件生成的客户只有 created_by，没有负责人关系。
-- 权限过滤后，这些客户连重复预检都不可见，于是同一公司的下一位联系人无法
-- 追加到原客户。与供应商的 00029 一致：仅为没有任何有效负责人的历史客户，
-- 把原建档人补成主负责人；已有人工分配的关系一律不碰。
INSERT INTO customer_owners (
    tenant_id, customer_id, employee_id, employee_name,
    responsibility_code, is_primary, created_by, updated_by
)
SELECT
    c.tenant_id,
    c.id,
    c.created_by,
    COALESCE(
        NULLIF((
            SELECT ccl.operator_name
            FROM customer_change_logs ccl
            WHERE ccl.tenant_id = c.tenant_id
              AND ccl.customer_id = c.id
              AND ccl.operator_id = c.created_by
              AND btrim(ccl.operator_name) <> ''
            ORDER BY ccl.created_at, ccl.id
            LIMIT 1
        ), ''),
        '员工 ' || c.created_by::text
    ),
    'SALES',
    true,
    c.created_by,
    c.created_by
FROM customers c
WHERE c.created_by > 0
  AND NOT EXISTS (
      SELECT 1
      FROM customer_owners co
      WHERE co.tenant_id = c.tenant_id
        AND co.customer_id = c.id
        AND co.status = 'ACTIVE'
  )
ON CONFLICT DO NOTHING;

-- +goose Down
-- 无法可靠区分迁移补录与之后人工维护的负责人，回滚时保留权限关系。

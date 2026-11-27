-- +goose Up
INSERT INTO permissions(code,name,module,menu_path)
VALUES ('finance:reimbursement:approve','报销审批','finance','')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions(tenant_id,role_id,permission_id)
SELECT r.tenant_id,r.id,p.id
FROM roles r CROSS JOIN permissions p
WHERE p.code='finance:reimbursement:approve'
  AND (
    r.code IN ('FINANCE_MANAGER','SUPER_ADMIN','GENERAL_MANAGER')
    OR r.name='总经理'
    -- Older tenants use the built-in BOSS role for executive approval.
    OR (r.code='BOSS' AND NOT EXISTS (
      SELECT 1 FROM roles gm WHERE gm.tenant_id=r.tenant_id
        AND (gm.code='GENERAL_MANAGER' OR gm.name='总经理')
    ))
  )
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (
  SELECT id FROM permissions WHERE code='finance:reimbursement:approve'
);
DELETE FROM permissions WHERE code='finance:reimbursement:approve';

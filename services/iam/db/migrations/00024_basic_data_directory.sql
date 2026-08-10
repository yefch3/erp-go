-- +goose Up

-- B1 只扩展组织目录，不改变或删除现有部门、员工和账号数据。
ALTER TABLE departments
  ADD COLUMN leader_employee_id BIGINT,
  ADD COLUMN version INT NOT NULL DEFAULT 1;

ALTER TABLE employees
  ADD COLUMN english_name VARCHAR(100) NOT NULL DEFAULT '',
  ADD COLUMN hire_date DATE,
  ADD COLUMN leave_date DATE,
  ADD COLUMN remark TEXT NOT NULL DEFAULT '',
  ADD COLUMN version INT NOT NULL DEFAULT 1;

ALTER TABLE departments
  ADD CONSTRAINT departments_leader_employee_fk
  FOREIGN KEY (leader_employee_id) REFERENCES employees(id);

CREATE INDEX departments_parent_idx
  ON departments (tenant_id, parent_id, sort_order, id);
CREATE INDEX departments_leader_idx
  ON departments (tenant_id, leader_employee_id)
  WHERE leader_employee_id IS NOT NULL;
CREATE INDEX employees_manager_active_idx
  ON employees (tenant_id, manager_id)
  WHERE status = 'ACTIVE';

CREATE TABLE directory_change_logs (
  id          BIGSERIAL PRIMARY KEY,
  tenant_id   BIGINT       NOT NULL,
  entity_type VARCHAR(32)  NOT NULL CHECK (entity_type IN ('DEPARTMENT', 'EMPLOYEE')),
  entity_id   BIGINT       NOT NULL,
  action      VARCHAR(32)  NOT NULL,
  before_data JSONB        NOT NULL DEFAULT '{}'::jsonb,
  after_data  JSONB        NOT NULL DEFAULT '{}'::jsonb,
  operator_id BIGINT       NOT NULL,
  created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX directory_change_logs_entity_idx
  ON directory_change_logs (tenant_id, entity_type, entity_id, id DESC);

INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('iam:department:read',  '查看部门', 'iam', '/basic/employees/departments'),
  ('iam:department:write', '维护部门', 'iam', '')
ON CONFLICT (code) DO NOTHING;

-- 已经拥有员工目录权限的角色自动获得对应部门权限，避免升级后菜单突然消失。
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT DISTINCT rp.tenant_id, rp.role_id, target.id
FROM role_permissions rp
JOIN permissions source ON source.id = rp.permission_id
JOIN permissions target ON target.code = CASE source.code
  WHEN 'iam:employee:read' THEN 'iam:department:read'
  WHEN 'iam:employee:write' THEN 'iam:department:write'
END
WHERE source.code IN ('iam:employee:read', 'iam:employee:write')
ON CONFLICT DO NOTHING;

-- +goose Down

DELETE FROM role_permissions
WHERE permission_id IN (
  SELECT id FROM permissions WHERE code IN ('iam:department:read', 'iam:department:write')
);
DELETE FROM permissions WHERE code IN ('iam:department:read', 'iam:department:write');

DROP TABLE directory_change_logs;
DROP INDEX employees_manager_active_idx;
DROP INDEX departments_leader_idx;
DROP INDEX departments_parent_idx;

ALTER TABLE departments DROP CONSTRAINT departments_leader_employee_fk;
ALTER TABLE departments
  DROP COLUMN leader_employee_id,
  DROP COLUMN version;
ALTER TABLE employees
  DROP COLUMN english_name,
  DROP COLUMN hire_date,
  DROP COLUMN leave_date,
  DROP COLUMN remark,
  DROP COLUMN version;

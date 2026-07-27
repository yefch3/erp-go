-- +goose Up
CREATE TABLE departments (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   BIGINT       NOT NULL DEFAULT 1,
    code        VARCHAR(50)  NOT NULL,
    name        VARCHAR(100) NOT NULL,
    parent_id   BIGINT REFERENCES departments(id),
    path        VARCHAR(500) NOT NULL DEFAULT '/',
    level       INT          NOT NULL DEFAULT 1,
    sort_order  INT          NOT NULL DEFAULT 0,
    status      VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, code)
);

CREATE TABLE employees (
    id            BIGSERIAL PRIMARY KEY,
    tenant_id     BIGINT       NOT NULL DEFAULT 1,
    code          VARCHAR(50)  NOT NULL,
    name          VARCHAR(100) NOT NULL,
    department_id BIGINT       NOT NULL REFERENCES departments(id),
    position      VARCHAR(100) NOT NULL DEFAULT '',
    email         VARCHAR(200) NOT NULL DEFAULT '',
    phone         VARCHAR(50)  NOT NULL DEFAULT '',
    status        VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE','LEFT')),
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, code)
);
CREATE INDEX employees_dept_idx ON employees (tenant_id, department_id) WHERE status = 'ACTIVE';

CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    tenant_id     BIGINT       NOT NULL DEFAULT 1,
    employee_id   BIGINT       NOT NULL UNIQUE REFERENCES employees(id),
    username      VARCHAR(100) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    status        VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','LOCKED','DISABLED')),
    failed_count  INT          NOT NULL DEFAULT 0,
    last_login_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, username)
);

CREATE TABLE roles (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   BIGINT       NOT NULL DEFAULT 1,
    code        VARCHAR(50)  NOT NULL,
    name        VARCHAR(100) NOT NULL,
    description TEXT         NOT NULL DEFAULT '',
    status      VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, code)
);

-- Global permission dictionary: system-defined, identical for every tenant,
-- seeded by migrations only. Deliberately tenant-free (exempted in CI check).
CREATE TABLE permissions (
    id        BIGSERIAL PRIMARY KEY,
    code      VARCHAR(100) NOT NULL UNIQUE,
    name      VARCHAR(100) NOT NULL,
    module    VARCHAR(50)  NOT NULL,
    menu_path VARCHAR(200) NOT NULL DEFAULT ''
);

CREATE TABLE role_permissions (
    tenant_id     BIGINT NOT NULL DEFAULT 1,
    role_id       BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id BIGINT NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (tenant_id, role_id, permission_id)
);

CREATE TABLE employee_roles (
    tenant_id   BIGINT NOT NULL DEFAULT 1,
    employee_id BIGINT NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    role_id     BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    PRIMARY KEY (tenant_id, employee_id, role_id)
);

CREATE TABLE role_data_scopes (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   BIGINT      NOT NULL DEFAULT 1,
    role_id     BIGINT      NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    module      VARCHAR(50) NOT NULL,
    scope_type  VARCHAR(32) NOT NULL CHECK (scope_type IN ('SELF','DEPT','DEPT_AND_SUB','ALL','CUSTOM')),
    custom_dept_ids BIGINT[],
    UNIQUE (tenant_id, role_id, module)
);

-- Permission dictionary seed. Codes are module:resource:action; grow with
-- later migrations, never edit or reuse existing codes.
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('iam:employee:read',    '查看员工',   'iam', '/settings/employees'),
  ('iam:employee:write',   '维护员工',   'iam', ''),
  ('iam:role:read',        '查看角色',   'iam', '/settings/roles'),
  ('iam:role:write',       '维护角色',   'iam', ''),
  ('masterdata:customer:read',  '查看客户', 'masterdata', '/basic/customers'),
  ('masterdata:customer:write', '维护客户', 'masterdata', ''),
  ('masterdata:supplier:read',  '查看供应商', 'masterdata', '/basic/suppliers'),
  ('masterdata:supplier:write', '维护供应商', 'masterdata', ''),
  ('product:product:read',  '查看产品', 'product', '/products'),
  ('product:product:write', '维护产品', 'product', ''),
  ('export:quotation:read',  '查看报价单', 'export', '/export/quotations'),
  ('export:quotation:write', '维护报价单', 'export', ''),
  ('export:contract:read',    '查看合同', 'export', '/export/contracts'),
  ('export:contract:write',   '维护合同', 'export', ''),
  ('export:contract:approve', '审批合同', 'export', ''),
  ('fx:rate:read',   '查看汇率', 'fx', '/fx'),
  ('fx:rate:write',  '手工录入汇率', 'fx', ''),
  ('approval:task:act', '处理审批任务', 'approval', '/todos');

-- +goose Down
DROP TABLE role_data_scopes;
DROP TABLE employee_roles;
DROP TABLE role_permissions;
DROP TABLE permissions;
DROP TABLE roles;
DROP TABLE users;
DROP TABLE employees;
DROP TABLE departments;

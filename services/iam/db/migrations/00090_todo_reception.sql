-- +goose Up
CREATE TABLE role_todo_reception (
    tenant_id bigint NOT NULL,
    role_id bigint NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    approvals_only boolean NOT NULL DEFAULT false,
    categories text[] NOT NULL DEFAULT '{}',
    PRIMARY KEY (tenant_id, role_id)
);

CREATE TABLE employee_todo_reception (
    tenant_id bigint NOT NULL,
    employee_id bigint NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    PRIMARY KEY (tenant_id, employee_id)
);

-- +goose Down
DROP TABLE employee_todo_reception;
DROP TABLE role_todo_reception;

-- +goose Up

-- Who someone reports to. Kept on the employee rather than as a department
-- head, because "my manager" and "the head of my department" are not the same
-- person often enough to matter: matrix reporting, deputies, and people who
-- sit in one department but report into another.
--
-- Self-referencing and nullable: the person at the top reports to nobody.
ALTER TABLE employees ADD COLUMN manager_id BIGINT REFERENCES employees(id);
CREATE INDEX employees_manager_idx ON employees (tenant_id, manager_id);

-- +goose Down
DROP INDEX employees_manager_idx;
ALTER TABLE employees DROP COLUMN manager_id;

-- +goose Up
-- A node can now target whoever the submitter reports to, not only a fixed
-- role or a named person. Which of the three a flow uses is a policy choice:
-- roles survive staff changes, a manager node follows the org chart.
ALTER TABLE approval_nodes DROP CONSTRAINT approval_nodes_approver_type_check;
ALTER TABLE approval_nodes ADD CONSTRAINT approval_nodes_approver_type_check
    CHECK (approver_type IN ('ROLE', 'EMPLOYEE', 'MANAGER'));

-- +goose Down
ALTER TABLE approval_nodes DROP CONSTRAINT approval_nodes_approver_type_check;
ALTER TABLE approval_nodes ADD CONSTRAINT approval_nodes_approver_type_check
    CHECK (approver_type IN ('ROLE', 'EMPLOYEE'));

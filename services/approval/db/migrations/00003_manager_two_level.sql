-- +goose Up
-- Contracts now climb the reporting line instead of naming roles: level 1 is
-- the submitter's manager, level 2 that person's manager. approver_ref holds
-- how far up, so one node type covers both.
--
-- A level with nobody in it is skipped, not failed: the chain runs out at the
-- top of the company, and the person up there has nobody to ask.
UPDATE approval_nodes SET name = '直属上级审批', approver_type = 'MANAGER', approver_ref = 1
WHERE definition_id = 1 AND seq = 1;
UPDATE approval_nodes SET name = '上级的上级审批', approver_type = 'MANAGER', approver_ref = 2
WHERE definition_id = 1 AND seq = 2;

-- +goose Down
UPDATE approval_nodes SET name = '销售主管审批', approver_type = 'ROLE', approver_ref = 3
WHERE definition_id = 1 AND seq = 1;
UPDATE approval_nodes SET name = '总经理审批', approver_type = 'ROLE', approver_ref = 1
WHERE definition_id = 1 AND seq = 2;

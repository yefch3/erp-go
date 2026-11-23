-- +goose Up
-- Replace the untouched starter large-order flow for new submissions only.
-- Running and historical instances still reference their original definition.
WITH starter AS (
    SELECT d.id, d.tenant_id, d.biz_type, d.name, d.min_amount, first_node.name AS node_name
    FROM approval_definitions d
    JOIN approval_nodes first_node ON first_node.tenant_id = d.tenant_id AND first_node.definition_id = d.id
        AND first_node.seq = 1 AND first_node.approver_type = 'MANAGER'
        AND first_node.approver_ref = 1 AND first_node.approve_mode = 'ANY'
    JOIN approval_nodes second_node ON second_node.tenant_id = d.tenant_id AND second_node.definition_id = d.id
        AND second_node.seq = 2 AND second_node.approver_type = 'MANAGER'
        AND second_node.approver_ref = 2 AND second_node.approve_mode = 'ANY'
    WHERE d.biz_type = 'PURCHASE_ORDER' AND d.status = 'ACTIVE' AND d.min_amount = 50000
      AND d.version = 1 AND d.created_by = 0 AND d.name = '采购单审批（大额）'
      AND NOT EXISTS (
          SELECT 1 FROM approval_nodes n
          WHERE n.tenant_id = d.tenant_id AND n.definition_id = d.id AND n.seq NOT IN (1, 2)
      )
), replacement AS (
    INSERT INTO approval_definitions (tenant_id, biz_type, name, version, min_amount, status)
    SELECT s.tenant_id, s.biz_type, s.name,
           (SELECT max(d.version) + 1 FROM approval_definitions d
            WHERE d.tenant_id = s.tenant_id AND d.biz_type = s.biz_type AND d.min_amount = s.min_amount),
           s.min_amount, 'ACTIVE'
    FROM starter s
    RETURNING tenant_id, id
), copied AS (
    INSERT INTO approval_nodes (tenant_id, definition_id, seq, name, approver_type, approver_ref, approve_mode)
    SELECT r.tenant_id, r.id, 1, s.node_name, 'MANAGER', 1, 'ANY'
    FROM replacement r JOIN starter s ON s.tenant_id = r.tenant_id
    RETURNING tenant_id
)
UPDATE approval_definitions d SET status = 'INACTIVE'
FROM starter s JOIN copied c ON c.tenant_id = s.tenant_id
WHERE d.tenant_id = s.tenant_id AND d.id = s.id;

-- +goose Down
-- Keep the new flow versions attached to any decisions made under them.
SELECT 1;

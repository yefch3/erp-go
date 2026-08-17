-- +goose Up

-- Supplier confirmations that differ from the approved purchase order are
-- reviewed independently. The original order stays unchanged; the approval
-- instance and confirmation record retain the proposed values for audit.
INSERT INTO approval_definitions (tenant_id, biz_type, name, version, min_amount) VALUES
  (1, 'PURCHASE_ORDER_CHANGE', '采购单供应商差异确认审批', 1, 0);

INSERT INTO approval_nodes (tenant_id, definition_id, seq, name, approver_type, approver_ref, approve_mode)
SELECT 1, d.id, 1, '直属上级审批', 'MANAGER', 1, 'ANY'
FROM approval_definitions d
WHERE d.tenant_id = 1
  AND d.biz_type = 'PURCHASE_ORDER_CHANGE'
  AND d.version = 1
  AND d.min_amount = 0;

-- +goose Down
DELETE FROM approval_nodes WHERE definition_id IN
  (SELECT id FROM approval_definitions WHERE tenant_id = 1 AND biz_type = 'PURCHASE_ORDER_CHANGE');
DELETE FROM approval_definitions WHERE tenant_id = 1 AND biz_type = 'PURCHASE_ORDER_CHANGE';

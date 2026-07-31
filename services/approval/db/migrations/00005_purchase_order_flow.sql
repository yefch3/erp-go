-- +goose Up

-- A starter flow for purchase orders, so the first one submitted has
-- something to run through instead of failing on AP_DEFINITION_NOT_FOUND.
--
-- Two bands, because the reason to approve a purchase at all is the amount:
-- a 500-yuan restock and a 500,000-yuan commitment should not need the same
-- signatures. Small orders stop at the direct manager; large ones add a
-- second level. Both are data — a company reshapes them on the flow designer
-- page without touching code.
INSERT INTO approval_definitions (tenant_id, biz_type, name, version, min_amount) VALUES
  (1, 'PURCHASE_ORDER', '采购单审批', 1, 0),
  (1, 'PURCHASE_ORDER', '采购单审批（大额）', 1, 50000);

INSERT INTO approval_nodes (tenant_id, definition_id, seq, name, approver_type, approver_ref, approve_mode)
SELECT 1, d.id, 1, '直属上级审批', 'MANAGER', 1, 'ANY'
FROM approval_definitions d WHERE d.biz_type = 'PURCHASE_ORDER' AND d.min_amount = 0
UNION ALL
SELECT 1, d.id, 1, '直属上级审批', 'MANAGER', 1, 'ANY'
FROM approval_definitions d WHERE d.biz_type = 'PURCHASE_ORDER' AND d.min_amount = 50000
UNION ALL
SELECT 1, d.id, 2, '上级的上级审批', 'MANAGER', 2, 'ANY'
FROM approval_definitions d WHERE d.biz_type = 'PURCHASE_ORDER' AND d.min_amount = 50000;

-- +goose Down
DELETE FROM approval_nodes WHERE definition_id IN
  (SELECT id FROM approval_definitions WHERE biz_type = 'PURCHASE_ORDER');
DELETE FROM approval_definitions WHERE biz_type = 'PURCHASE_ORDER';

-- +goose Up
ALTER TABLE approval_nodes DROP CONSTRAINT approval_nodes_approver_type_check;
ALTER TABLE approval_nodes ADD CONSTRAINT approval_nodes_approver_type_check
  CHECK (approver_type IN ('ROLE','EMPLOYEE','MANAGER','DEPARTMENT_LEADER','FINANCE_MANAGER'));

INSERT INTO approval_definitions (tenant_id,biz_type,name,version,status,min_amount,created_by)
SELECT DISTINCT d.tenant_id,'TRAVEL_REIMBURSEMENT','出差报销审批',1,'ACTIVE',0,0
FROM approval_definitions d
WHERE NOT EXISTS (SELECT 1 FROM approval_definitions x WHERE x.tenant_id=d.tenant_id AND x.biz_type='TRAVEL_REIMBURSEMENT');

INSERT INTO approval_nodes (tenant_id,definition_id,seq,name,approver_type,approver_ref,approve_mode)
SELECT d.tenant_id,d.id,v.seq,
       CASE v.seq WHEN 1 THEN '部门负责人确认' ELSE '财务负责人审批' END,
       CASE v.seq WHEN 1 THEN 'DEPARTMENT_LEADER' ELSE 'FINANCE_MANAGER' END,
       0,'ANY'
FROM approval_definitions d
CROSS JOIN generate_series(1,2) v(seq)
WHERE d.biz_type='TRAVEL_REIMBURSEMENT' AND d.status='ACTIVE'
AND NOT EXISTS (SELECT 1 FROM approval_nodes n WHERE n.tenant_id=d.tenant_id AND n.definition_id=d.id AND n.seq=v.seq);

-- +goose Down
DELETE FROM approval_nodes WHERE definition_id IN (SELECT id FROM approval_definitions WHERE biz_type='TRAVEL_REIMBURSEMENT');
DELETE FROM approval_definitions WHERE biz_type='TRAVEL_REIMBURSEMENT';
ALTER TABLE approval_nodes DROP CONSTRAINT approval_nodes_approver_type_check;
ALTER TABLE approval_nodes ADD CONSTRAINT approval_nodes_approver_type_check
  CHECK (approver_type IN ('ROLE','EMPLOYEE','MANAGER'));

-- +goose Up
INSERT INTO approval_definitions (tenant_id,biz_type,name,version,min_amount)
SELECT DISTINCT tenant_id,'SHIPPING_REQUOTE','正式物流方案确认',1,0 FROM approval_definitions d WHERE NOT EXISTS (SELECT 1 FROM approval_definitions existing WHERE existing.tenant_id=d.tenant_id AND existing.biz_type='SHIPPING_REQUOTE');
INSERT INTO approval_nodes (tenant_id,definition_id,seq,name,approver_type,approver_ref,approve_mode)
SELECT tenant_id,id,1,'直属上级确认','MANAGER',1,'ANY'
FROM approval_definitions WHERE biz_type='SHIPPING_REQUOTE' AND min_amount=0;

-- +goose Down
DELETE FROM approval_nodes WHERE definition_id IN
  (SELECT id FROM approval_definitions WHERE biz_type='SHIPPING_REQUOTE');
DELETE FROM approval_definitions WHERE biz_type='SHIPPING_REQUOTE';

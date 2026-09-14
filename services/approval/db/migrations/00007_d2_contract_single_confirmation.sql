-- +goose Up
UPDATE approval_definitions SET status='INACTIVE' WHERE biz_type='CONTRACT' AND status='ACTIVE';
INSERT INTO approval_definitions(tenant_id,biz_type,name,version,status,min_amount)
SELECT tenant_id,'CONTRACT','外销合同上级确认',MAX(version)+1,'ACTIVE',0
FROM approval_definitions WHERE biz_type='CONTRACT' GROUP BY tenant_id;
INSERT INTO approval_nodes(tenant_id,definition_id,seq,name,approver_type,approver_ref,approve_mode)
SELECT tenant_id,id,1,'上级确认','MANAGER',1,'ANY' FROM approval_definitions
WHERE biz_type='CONTRACT' AND status='ACTIVE';
-- +goose Down
-- Keep completed and running decisions intact; do not silently reintroduce a second approval.
SELECT 1;

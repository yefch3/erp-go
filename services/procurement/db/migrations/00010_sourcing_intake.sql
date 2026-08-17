-- +goose Up

-- 标准化询盘先进入待确认队列。员工确认后才进入原有的询价复核流程，
-- 避免邮件自动转入或手工上传时直接创建可对外询价的数据。
ALTER TABLE sourcing_cases DROP CONSTRAINT sourcing_cases_status_check;
ALTER TABLE sourcing_cases
    ADD CONSTRAINT sourcing_cases_status_check
    CHECK (status IN ('INTAKE_PENDING', 'REVIEWING', 'SOURCING', 'QUOTES_RECEIVED',
                      'COSTING', 'CUSTOMER_QUOTE_CREATED', 'CANCELLED'));
ALTER TABLE sourcing_cases ALTER COLUMN status SET DEFAULT 'INTAKE_PENDING';

-- 手工上传没有邮件附件 ID。网关把文件内容的 SHA-256 摘要折叠为正整数，
-- 并以 source_mail_id=-1 标记手工来源；同一租户不能重复导入完全相同的文件。
CREATE UNIQUE INDEX sourcing_cases_manual_file_idx
    ON sourcing_cases (tenant_id, source_attachment_id)
    WHERE source_mail_id = -1 AND source_attachment_id > 0;

-- +goose Down
ALTER TABLE sourcing_cases ALTER COLUMN status SET DEFAULT 'REVIEWING';
DROP INDEX IF EXISTS sourcing_cases_manual_file_idx;
UPDATE sourcing_cases SET status = 'REVIEWING' WHERE status = 'INTAKE_PENDING';
ALTER TABLE sourcing_cases DROP CONSTRAINT sourcing_cases_status_check;
ALTER TABLE sourcing_cases
    ADD CONSTRAINT sourcing_cases_status_check
    CHECK (status IN ('REVIEWING', 'SOURCING', 'QUOTES_RECEIVED',
                      'COSTING', 'CUSTOMER_QUOTE_CREATED', 'CANCELLED'));

-- +goose Up
-- P7 将“发邮件 RFQ”扩展为完整的工厂询价记录，兼容电话、微信和线下沟通。
ALTER TABLE factory_rfqs ADD COLUMN inquiry_channel VARCHAR(24) NOT NULL DEFAULT 'SYSTEM_EMAIL'
  CHECK (inquiry_channel IN ('SYSTEM_EMAIL','PHONE','WECHAT','WHATSAPP','IN_PERSON','OTHER'));
ALTER TABLE factory_rfqs ADD COLUMN contact_name VARCHAR(150) NOT NULL DEFAULT '';
ALTER TABLE factory_rfqs ADD COLUMN contact_value VARCHAR(320) NOT NULL DEFAULT '';
ALTER TABLE factory_rfqs ADD COLUMN contacted_at TIMESTAMPTZ;
ALTER TABLE factory_rfqs ADD COLUMN inquiry_note TEXT NOT NULL DEFAULT '';
ALTER TABLE factory_rfqs ADD COLUMN round_no INT NOT NULL DEFAULT 1 CHECK (round_no > 0);

-- 每次报价都新增版本，禁止覆盖旧报价；口头报价必须明确标记确认状态。
ALTER TABLE supplier_quotes DROP CONSTRAINT supplier_quotes_source_check;
ALTER TABLE supplier_quotes ADD CONSTRAINT supplier_quotes_source_check
  CHECK (source IN ('MANUAL','EXCEL_IMPORT','EMAIL_ATTACHMENT','PHONE','WECHAT','WHATSAPP','IN_PERSON','OTHER'));
ALTER TABLE supplier_quotes ADD COLUMN version_no INT NOT NULL DEFAULT 1 CHECK (version_no > 0);
ALTER TABLE supplier_quotes ADD COLUMN confirmation_status VARCHAR(32) NOT NULL DEFAULT 'WRITTEN_CONFIRMED'
  CHECK (confirmation_status IN ('VERBAL_PENDING','WRITTEN_CONFIRMED','INVALID','CANCELLED'));
ALTER TABLE supplier_quotes ADD COLUMN evidence_note TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX supplier_quotes_rfq_version_idx
  ON supplier_quotes (tenant_id, factory_rfq_id, version_no);

-- +goose Down
DROP INDEX supplier_quotes_rfq_version_idx;
ALTER TABLE supplier_quotes DROP COLUMN evidence_note;
ALTER TABLE supplier_quotes DROP COLUMN confirmation_status;
ALTER TABLE supplier_quotes DROP COLUMN version_no;
ALTER TABLE supplier_quotes DROP CONSTRAINT supplier_quotes_source_check;
ALTER TABLE supplier_quotes ADD CONSTRAINT supplier_quotes_source_check
  CHECK (source IN ('MANUAL','EXCEL_IMPORT','EMAIL_ATTACHMENT'));
ALTER TABLE factory_rfqs DROP COLUMN round_no;
ALTER TABLE factory_rfqs DROP COLUMN inquiry_note;
ALTER TABLE factory_rfqs DROP COLUMN contacted_at;
ALTER TABLE factory_rfqs DROP COLUMN contact_value;
ALTER TABLE factory_rfqs DROP COLUMN contact_name;
ALTER TABLE factory_rfqs DROP COLUMN inquiry_channel;

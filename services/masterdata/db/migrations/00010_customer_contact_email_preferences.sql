-- +goose Up

-- B2：为客户联系人补充邮件接收偏好。旧联系人默认保持可接收，避免升级后从现有通讯录中消失。
ALTER TABLE customer_contacts
    ADD COLUMN email_permission VARCHAR(32) NOT NULL DEFAULT 'ALLOWED',
    ADD COLUMN email_categories TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    ADD CONSTRAINT customer_contacts_email_permission_check
        CHECK (email_permission IN ('ALLOWED', 'OPTED_OUT', 'INVALID'));

CREATE INDEX customer_contacts_mailable_idx
    ON customer_contacts (tenant_id, customer_id, sort_order, id)
    WHERE status = 'ACTIVE' AND email_permission = 'ALLOWED' AND email <> '';

-- +goose Down

DROP INDEX IF EXISTS customer_contacts_mailable_idx;
ALTER TABLE customer_contacts
    DROP CONSTRAINT IF EXISTS customer_contacts_email_permission_check,
    DROP COLUMN IF EXISTS email_categories,
    DROP COLUMN IF EXISTS email_permission;

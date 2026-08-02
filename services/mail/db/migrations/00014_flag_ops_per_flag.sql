-- +goose Up

-- The queue held one pending intent per message, which was right while read
-- state was the only thing being published. Starring is a second, independent
-- flag: with one row per message, starring a mail would silently replace a
-- read change that had not gone up yet, and that change would never reach the
-- host at all.
--
-- One row per message *per flag* instead. Collapsing still happens where it
-- should — toggling a star five times still publishes once — without one
-- kind of change erasing another.
ALTER TABLE mail_flag_ops
    ADD COLUMN flag VARCHAR(16) NOT NULL DEFAULT 'SEEN';

ALTER TABLE mail_flag_ops
    DROP CONSTRAINT mail_flag_ops_tenant_id_account_id_folder_imap_uid_key;

ALTER TABLE mail_flag_ops
    ADD CONSTRAINT mail_flag_ops_one_per_flag
    UNIQUE (tenant_id, account_id, folder, imap_uid, flag);

-- +goose Down
ALTER TABLE mail_flag_ops DROP CONSTRAINT mail_flag_ops_one_per_flag;
ALTER TABLE mail_flag_ops DROP COLUMN flag;
ALTER TABLE mail_flag_ops
    ADD CONSTRAINT mail_flag_ops_tenant_id_account_id_folder_imap_uid_key
    UNIQUE (tenant_id, account_id, folder, imap_uid);

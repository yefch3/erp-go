-- +goose Up

-- 员工自建的邮件文件夹（Issue #362）。
--
-- 文件夹**真的建在邮件服务器上**（IMAP CREATE），不是 ERP 里的一个标签：
-- 员工在 Foxmail 里也看得到同一个文件夹、同一批信。这一行只是我们对它的
-- 登记——名字、属于哪个信箱、谁建的。host_name 是服务器上的实际名字，和
-- email_inbound.folder 里存的一致；name 是给人看的。
CREATE TABLE mail_folders (
    id          BIGSERIAL    PRIMARY KEY,
    tenant_id   BIGINT       NOT NULL,
    account_id  BIGINT       NOT NULL,
    name        VARCHAR(120) NOT NULL,
    host_name   VARCHAR(255) NOT NULL,
    created_by  BIGINT       NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, account_id, host_name)
);
CREATE INDEX mail_folders_account_idx ON mail_folders (tenant_id, account_id, name);

-- +goose StatementBegin
-- 自建文件夹在会话视图里有自己的一档：'F:' 加服务器上的名字。
--
-- 从前 INBOX / JUNK 以外一律 NULL（不进任何视图），那是"只有系统文件夹"的
-- 年代的规矩。现在信被挪进自建文件夹之后，行的 folder 就是那个名字，视图跟着
-- 变成 'F:名字'——列表按同一个 key 过滤。顺序有讲究：删除优先于一切（自建
-- 文件夹里删的信也进回收站）；SENT 仍然不进会话视图，它有自己的列表。
CREATE OR REPLACE FUNCTION mail_view_of(
    p_folder TEXT, p_not_junk BOOLEAN, p_is_bounce BOOLEAN,
    p_archived TIMESTAMPTZ, p_deleted TIMESTAMPTZ
) RETURNS TEXT
LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE
        WHEN p_is_bounce THEN NULL
        WHEN p_folder = 'SENT' THEN NULL
        WHEN p_deleted IS NOT NULL THEN 'TRASH'
        WHEN p_folder = 'JUNK' AND NOT p_not_junk THEN 'JUNK'
        WHEN p_folder = 'JUNK' THEN CASE WHEN p_archived IS NOT NULL THEN 'ARCHIVE' ELSE 'INBOX' END
        WHEN p_folder <> 'INBOX' THEN 'F:' || p_folder
        WHEN p_archived IS NOT NULL THEN 'ARCHIVE'
        ELSE 'INBOX'
    END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- 和 00034 一模一样，只有星标那一档的 IN 列表多认了自建文件夹：星在自建
-- 文件夹里的信也该出现在「星标邮件」里。
CREATE OR REPLACE FUNCTION mail_thread_view_refresh(p_tenant BIGINT, p_owner BIGINT, p_account BIGINT, p_group TEXT)
RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM mail_thread_view
    WHERE tenant_id = p_tenant AND owner_id = p_owner AND account_id = p_account AND group_key = p_group;

    INSERT INTO mail_thread_view (
        tenant_id, owner_id, account_id, group_key, view,
        msg_count, last_id, last_at, any_unread, any_starred, any_attachment)
    SELECT p_tenant, p_owner, p_account, p_group, v.view,
           count(*)::int,
           (array_agg(v.id ORDER BY v.at DESC, v.id DESC))[1],
           max(v.at),
           bool_or(NOT v.is_read),
           bool_or(v.is_starred),
           bool_or(v.has_attachments)
    FROM (
        SELECT m.id, m.is_read, m.is_starred, m.has_attachments,
               m.received_at AS at,
               mail_view_of(m.folder, m.not_junk, m.is_bounce,
                            m.archived_at, m.deleted_at) AS view
        FROM email_inbound m
        WHERE m.tenant_id = p_tenant AND m.owner_id = p_owner AND m.account_id = p_account
          AND coalesce(nullif(m.thread_key, ''), 'm:' || m.id::text) = p_group

        UNION ALL

        SELECT m.id, m.is_read, m.is_starred, m.has_attachments,
               m.received_at, 'STARRED'
        FROM email_inbound m
        WHERE m.tenant_id = p_tenant AND m.owner_id = p_owner AND m.account_id = p_account
          AND coalesce(nullif(m.thread_key, ''), 'm:' || m.id::text) = p_group
          AND m.is_starred
          AND (mail_view_of(m.folder, m.not_junk, m.is_bounce, m.archived_at, m.deleted_at) IN ('INBOX', 'ARCHIVE')
               OR mail_view_of(m.folder, m.not_junk, m.is_bounce, m.archived_at, m.deleted_at) LIKE 'F:%')
    ) v
    WHERE v.view IS NOT NULL
    GROUP BY v.view;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS mail_folders;
-- 视图函数不回退：老定义对自建文件夹返回 NULL，只是让那些信从视图里消失，
-- 不会坏数据；真要回退，重跑 00034/00044 里的定义。

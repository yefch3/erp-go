-- +goose Up

-- 会话视图只在会影响它的字段变了时才重算。
--
-- mail_thread_view_update 触发器挂在 AFTER UPDATE ON email_inbound 上，任何
-- 一次更新都会把涉及的会话删掉重插一遍。可 email_inbound 上大多数更新和会话
-- 视图毫无关系：图片缓存盖章（每封信一次）、记下服务器上的位置、补回原件
-- 路径、挂客户……每一次都白白删插，生产上 3.3 万行的视图表占了 19 MB，一大半
-- 是这样留下的死行。
--
-- **为什么不写成 AFTER UPDATE OF 列名。** 那是最直接的写法，但 Postgres 不
-- 允许：带 transition table（REFERENCING OLD TABLE ... NEW TABLE）的 UPDATE
-- 触发器不能再带列清单（"transition tables cannot be specified for triggers
-- with column lists"，2026-09-23 实测）。而这个触发器离不开 transition
-- table——一条语句改几千行时，它按会话去重后只刷一次。所以改在函数里：
-- 按 id 把新旧两行对上，只留下相关字段真的变了的那些。
--
-- **相关字段 = mail_thread_view_refresh 读的全部字段**（00056 是它最后一版）：
--   分组       tenant_id, owner_id, account_id, thread_key（和 id）
--   落在哪个视图 folder, not_junk, is_bounce, archived_at, deleted_at（mail_view_of）
--   排序       received_at
--   聚合       is_read, is_starred, has_attachments
-- 以后给视图加列、或者 mail_view_of 多看一个字段，这里要跟着加一个——漏了
-- 的后果是那个字段变了而列表不变，而且不会报错。
--
-- 对不上的行（理论上只有改了 id 的更新，代码里没有）照旧两边都刷：宁可白
-- 刷一次，也不要漏刷。

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION mail_thread_view_sync_update() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    r RECORD;
BEGIN
    FOR r IN
        WITH changed AS (
            SELECT o.tenant_id AS o_tenant, o.owner_id AS o_owner, o.account_id AS o_account,
                   coalesce(nullif(o.thread_key, ''), 'm:' || o.id::text) AS o_group,
                   n.tenant_id AS n_tenant, n.owner_id AS n_owner, n.account_id AS n_account,
                   coalesce(nullif(n.thread_key, ''), 'm:' || n.id::text) AS n_group
            FROM old_rows o
            FULL JOIN new_rows n ON n.id = o.id
            WHERE o.id IS NULL OR n.id IS NULL
               OR (o.tenant_id, o.owner_id, o.account_id, o.thread_key,
                   o.folder, o.not_junk, o.is_bounce, o.archived_at, o.deleted_at,
                   o.received_at, o.is_read, o.is_starred, o.has_attachments)
                  IS DISTINCT FROM
                  (n.tenant_id, n.owner_id, n.account_id, n.thread_key,
                   n.folder, n.not_junk, n.is_bounce, n.archived_at, n.deleted_at,
                   n.received_at, n.is_read, n.is_starred, n.has_attachments)
        )
        SELECT DISTINCT tenant_id, owner_id, account_id, group_key FROM (
            SELECT o_tenant, o_owner, o_account, o_group FROM changed WHERE o_tenant IS NOT NULL
            UNION ALL
            SELECT n_tenant, n_owner, n_account, n_group FROM changed WHERE n_tenant IS NOT NULL
        ) g (tenant_id, owner_id, account_id, group_key)
    LOOP
        PERFORM mail_thread_view_refresh(r.tenant_id, r.owner_id, r.account_id, r.group_key);
    END LOOP;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose Down

-- 00044 那一版：每次更新都刷。
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION mail_thread_view_sync_update() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    r RECORD;
BEGIN
    FOR r IN
        SELECT DISTINCT tenant_id, owner_id, account_id, group_key FROM (
            SELECT tenant_id, owner_id, account_id,
                   coalesce(nullif(thread_key, ''), 'm:' || id::text) AS group_key
            FROM old_rows
            UNION ALL
            SELECT tenant_id, owner_id, account_id,
                   coalesce(nullif(thread_key, ''), 'm:' || id::text)
            FROM new_rows
        ) g
    LOOP
        PERFORM mail_thread_view_refresh(r.tenant_id, r.owner_id, r.account_id, r.group_key);
    END LOOP;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

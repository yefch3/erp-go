-- +goose Up

-- 会话按信箱分开：同一条会话落在两个信箱里，是两行，不是一行。
--
-- 00034 建这张表时「一人一箱」还是硬约束，所以 (租户, 人, 会话, 视图) 足够
-- 定位一行。00043 放开了那条约束，这个前提就没了：等绑定入口一上线，
-- 一个人可以既有 263 又有 Gmail，而客户可能同时抄送两个地址。那时候按老口径
-- 刷新，两个信箱的信会被 GROUP BY 合成一行——列表上显示「客户回了 4 次」，
-- 点进去是两个信箱混在一起的四封信，而左侧切到哪个信箱都是同一行。
--
-- 产品口径已经定了：**按邮箱分，左侧切换**，不做统一收件箱。所以维度加在
-- 主键上，不是加一个可选的筛选列。
--
-- 今天这张表里不会有任何跨信箱的行——还没有界面能给一个人加第二个信箱。
-- 先改口径，是因为那个界面上线的那一刻，错误的合并是**静默**发生的：
-- 行数对得上、约束不报错、日志里一个字都没有。
--
-- 顺序是 TRUNCATE → 改结构 → 重建，不是「改结构 → 重建」。加主键要拿
-- ACCESS EXCLUSIVE，而持锁期间任何 email_inbound 的写入都会因为触发器要动
-- 这张表而排队——收信轮询和 IDLE 推送整段停摆。空表上加主键是瞬时的，
-- 把锁的持有时间压到只剩最后那一次全表 INSERT。
--
-- **不能用 UPDATE ... SET account_id = (SELECT ...) 回填。** 那只是给现有行
-- 贴个标签，不会把一条跨两个信箱的合并行**拆成两行**：msg_count 仍是两个
-- 信箱之和，另一个信箱那一行根本不存在，而且行数对得上、约束也不报错。
-- 整表重建是唯一对的做法。
TRUNCATE mail_thread_view;

-- 空表，所以 NOT NULL 不必配 DEFAULT。刻意不给默认值：给了的话，将来某个
-- 忘了填 account_id 的 INSERT 会安静地写成 0，然后那一行在任何按信箱过滤的
-- 查询里都命中不了——又是一个不报错的错。
ALTER TABLE mail_thread_view ADD COLUMN account_id BIGINT NOT NULL;

ALTER TABLE mail_thread_view DROP CONSTRAINT mail_thread_view_pkey;
ALTER TABLE mail_thread_view
    ADD CONSTRAINT mail_thread_view_pkey
    PRIMARY KEY (tenant_id, owner_id, account_id, group_key, view);

-- 按信箱看列表时走这条。keyset 分页要靠索引直接读出顺序，account_id 如果只能
-- 当过滤条件，就退回成「扫这个人的全部会话再筛二十五条」——正是 00034 整篇
-- 注释在干掉的那个形状（70ms + 溢出磁盘的排序），而且信箱越多越慢，不报错。
CREATE INDEX mail_thread_view_account_list_idx
    ON mail_thread_view (tenant_id, owner_id, account_id, view, last_at DESC, last_id DESC);

-- 00034 那条旧索引**保留**。列表查询这一版的信箱筛选是可选的（不传 = 全部
-- 信箱），左侧切换器还没上线，前端今天什么都不传——那条路仍然要走得动。
-- 切换器上线、筛选变成必填之后，旧索引可以删，那是下一版的事。

-- +goose StatementBegin
-- 刷新函数加一个参数。
--
-- **必须先 DROP 再 CREATE。** CREATE OR REPLACE 改不了参数列表，它会安静地
-- 多留一个三参重载在库里，然后触发器按参数个数调到老的那个——迁移绿、测试
-- 绿，表却还按老口径刷新。migration_test.go 里的 assertFunctionArgs 专门数
-- 重载个数，就是为了让这种错当场变红。
DROP FUNCTION mail_thread_view_refresh(BIGINT, BIGINT, TEXT);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION mail_thread_view_refresh(
    p_tenant BIGINT, p_owner BIGINT, p_account BIGINT, p_group TEXT)
RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM mail_thread_view
    WHERE tenant_id = p_tenant AND owner_id = p_owner
      AND account_id = p_account AND group_key = p_group;

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
        -- WHERE 里 tenant_id + owner_id + 会话表达式的顺序不能动：
        -- email_inbound_thread_group_idx 正是这三列，account_id 加在后面是
        -- 前缀匹配之后的过滤，一条会话本来就只有个位数行。把 owner_id 换成
        -- account_id 的话索引前缀当场断掉，每次「标已读」从查一条会话变成
        -- 扫整个信箱（00034:92-95 记着这件事是怎么被发现的）。
        SELECT m.id, m.is_read, m.is_starred, m.has_attachments,
               m.received_at AS at,
               mail_view_of(m.folder, m.not_junk, m.is_bounce,
                            m.archived_at, m.deleted_at) AS view
        FROM email_inbound m
        WHERE m.tenant_id = p_tenant AND m.owner_id = p_owner
          AND coalesce(nullif(m.thread_key, ''), 'm:' || m.id::text) = p_group
          AND m.account_id = p_account

        UNION ALL

        SELECT m.id, m.is_read, m.is_starred, m.has_attachments,
               m.received_at, 'STARRED'
        FROM email_inbound m
        WHERE m.tenant_id = p_tenant AND m.owner_id = p_owner
          AND coalesce(nullif(m.thread_key, ''), 'm:' || m.id::text) = p_group
          AND m.account_id = p_account
          AND m.is_starred
          AND mail_view_of(m.folder, m.not_junk, m.is_bounce,
                           m.archived_at, m.deleted_at) IN ('INBOX', 'ARCHIVE')
    ) v
    WHERE v.view IS NOT NULL
    GROUP BY v.view;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- 两个触发器函数跟着换：DISTINCT 要带上 account_id，否则一封信落在 A 信箱
-- 只会去刷 A，而合并键变了之后，「刷哪一条」的答案也变了。
--
-- email_inbound.account_id 从 00005 起就是 NOT NULL，且没有任何一句 UPDATE
-- 会改它——所以 UPDATE 触发器里 old_rows 和 new_rows 的 account_id 永远相同，
-- 不存在「信搬到另一个信箱」这种情况要处理。
CREATE OR REPLACE FUNCTION mail_thread_view_sync() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    r RECORD;
BEGIN
    FOR r IN
        SELECT DISTINCT tenant_id, owner_id, account_id, group_key FROM (
            SELECT tenant_id, owner_id, account_id,
                   coalesce(nullif(thread_key, ''), 'm:' || id::text) AS group_key
            FROM affected_rows
        ) g
    LOOP
        PERFORM mail_thread_view_refresh(r.tenant_id, r.owner_id, r.account_id, r.group_key);
    END LOOP;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

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

-- 按新口径重建。和 00034:240-265 是同一段，只多了 account_id 进 SELECT 和
-- GROUP BY——用同一套函数算，历史邮件和新邮件才不会对「一条会话是什么」
-- 有两种说法。
INSERT INTO mail_thread_view (
    tenant_id, owner_id, account_id, group_key, view,
    msg_count, last_id, last_at, any_unread, any_starred, any_attachment)
SELECT tenant_id, owner_id, account_id, group_key, view,
       count(*)::int,
       (array_agg(id ORDER BY at DESC, id DESC))[1],
       max(at),
       bool_or(NOT is_read), bool_or(is_starred), bool_or(has_attachments)
FROM (
    SELECT tenant_id, owner_id, account_id,
           coalesce(nullif(thread_key, ''), 'm:' || id::text) AS group_key,
           mail_view_of(folder, not_junk, is_bounce, archived_at, deleted_at) AS view,
           id, received_at AS at, is_read, is_starred, has_attachments
    FROM email_inbound
    UNION ALL
    SELECT tenant_id, owner_id, account_id,
           coalesce(nullif(thread_key, ''), 'm:' || id::text),
           'STARRED',
           id, received_at, is_read, is_starred, has_attachments
    FROM email_inbound
    WHERE is_starred
      AND mail_view_of(folder, not_junk, is_bounce, archived_at, deleted_at)
          IN ('INBOX', 'ARCHIVE')
) v
WHERE view IS NOT NULL
GROUP BY tenant_id, owner_id, account_id, group_key, view;

-- +goose Down

-- 回滚要把 00034 的形状原样还回去，包括那个三参函数——00034 自己的 Down
-- 里写死了 DROP FUNCTION mail_thread_view_refresh(BIGINT, BIGINT, TEXT)，
-- 少还一个，回滚链就断在那里。

TRUNCATE mail_thread_view;

-- +goose StatementBegin
DROP FUNCTION mail_thread_view_refresh(BIGINT, BIGINT, BIGINT, TEXT);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION mail_thread_view_refresh(p_tenant BIGINT, p_owner BIGINT, p_group TEXT)
RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM mail_thread_view
    WHERE tenant_id = p_tenant AND owner_id = p_owner AND group_key = p_group;

    INSERT INTO mail_thread_view (
        tenant_id, owner_id, group_key, view,
        msg_count, last_id, last_at, any_unread, any_starred, any_attachment)
    SELECT p_tenant, p_owner, p_group, v.view,
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
        WHERE m.tenant_id = p_tenant AND m.owner_id = p_owner
          AND coalesce(nullif(m.thread_key, ''), 'm:' || m.id::text) = p_group

        UNION ALL

        SELECT m.id, m.is_read, m.is_starred, m.has_attachments,
               m.received_at, 'STARRED'
        FROM email_inbound m
        WHERE m.tenant_id = p_tenant AND m.owner_id = p_owner
          AND coalesce(nullif(m.thread_key, ''), 'm:' || m.id::text) = p_group
          AND m.is_starred
          AND mail_view_of(m.folder, m.not_junk, m.is_bounce,
                           m.archived_at, m.deleted_at) IN ('INBOX', 'ARCHIVE')
    ) v
    WHERE v.view IS NOT NULL
    GROUP BY v.view;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION mail_thread_view_sync() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    r RECORD;
BEGIN
    FOR r IN
        SELECT DISTINCT tenant_id, owner_id, group_key FROM (
            SELECT tenant_id, owner_id,
                   coalesce(nullif(thread_key, ''), 'm:' || id::text) AS group_key
            FROM affected_rows
        ) g
    LOOP
        PERFORM mail_thread_view_refresh(r.tenant_id, r.owner_id, r.group_key);
    END LOOP;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION mail_thread_view_sync_update() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    r RECORD;
BEGIN
    FOR r IN
        SELECT DISTINCT tenant_id, owner_id, group_key FROM (
            SELECT tenant_id, owner_id,
                   coalesce(nullif(thread_key, ''), 'm:' || id::text) AS group_key
            FROM old_rows
            UNION ALL
            SELECT tenant_id, owner_id,
                   coalesce(nullif(thread_key, ''), 'm:' || id::text)
            FROM new_rows
        ) g
    LOOP
        PERFORM mail_thread_view_refresh(r.tenant_id, r.owner_id, r.group_key);
    END LOOP;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

DROP INDEX mail_thread_view_account_list_idx;

ALTER TABLE mail_thread_view DROP CONSTRAINT mail_thread_view_pkey;
ALTER TABLE mail_thread_view DROP COLUMN account_id;
ALTER TABLE mail_thread_view
    ADD CONSTRAINT mail_thread_view_pkey
    PRIMARY KEY (tenant_id, owner_id, group_key, view);

INSERT INTO mail_thread_view (
    tenant_id, owner_id, group_key, view,
    msg_count, last_id, last_at, any_unread, any_starred, any_attachment)
SELECT tenant_id, owner_id, group_key, view,
       count(*)::int,
       (array_agg(id ORDER BY at DESC, id DESC))[1],
       max(at),
       bool_or(NOT is_read), bool_or(is_starred), bool_or(has_attachments)
FROM (
    SELECT tenant_id, owner_id,
           coalesce(nullif(thread_key, ''), 'm:' || id::text) AS group_key,
           mail_view_of(folder, not_junk, is_bounce, archived_at, deleted_at) AS view,
           id, received_at AS at, is_read, is_starred, has_attachments
    FROM email_inbound
    UNION ALL
    SELECT tenant_id, owner_id,
           coalesce(nullif(thread_key, ''), 'm:' || id::text),
           'STARRED',
           id, received_at, is_read, is_starred, has_attachments
    FROM email_inbound
    WHERE is_starred
      AND mail_view_of(folder, not_junk, is_bounce, archived_at, deleted_at)
          IN ('INBOX', 'ARCHIVE')
) v
WHERE view IS NOT NULL
GROUP BY tenant_id, owner_id, group_key, view;

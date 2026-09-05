-- name: GetMailHost :one
SELECT domain, smtp_host, smtp_port, smtp_security,
       imap_host, imap_port, imap_security,
       hourly_quota, daily_quota
FROM mail_hosts
WHERE tenant_id = sqlc.arg(tenant_id)::bigint;

-- name: UpsertMailHost :exec
INSERT INTO mail_hosts (
    tenant_id, domain, smtp_host, smtp_port, smtp_security,
    imap_host, imap_port, imap_security, hourly_quota, daily_quota, updated_at
) VALUES (
    sqlc.arg(tenant_id)::bigint, sqlc.arg(domain)::text,
    sqlc.arg(smtp_host)::text, sqlc.arg(smtp_port)::int, sqlc.arg(smtp_security)::text,
    sqlc.arg(imap_host)::text, sqlc.arg(imap_port)::int, sqlc.arg(imap_security)::text,
    sqlc.arg(hourly_quota)::int, sqlc.arg(daily_quota)::int, now()
)
ON CONFLICT (tenant_id) DO UPDATE SET
    domain = excluded.domain,
    smtp_host = excluded.smtp_host,
    smtp_port = excluded.smtp_port,
    smtp_security = excluded.smtp_security,
    imap_host = excluded.imap_host,
    imap_port = excluded.imap_port,
    imap_security = excluded.imap_security,
    hourly_quota = excluded.hourly_quota,
    daily_quota = excluded.daily_quota,
    updated_at = now();

-- name: GetMailAccountByEmail :one
-- 按**地址**取一个信箱。绑定路径用它回答两个问题：这个地址已经有行了吗，
-- 以及它是不是这个人的。
--
-- 一并回 employee_id，是因为「已经属于别人」和「不存在」要给出不同的话，
-- 而调用方必须自己比一次——查询按 (tenant, email) 唯一，不带 employee_id，
-- 否则别人已经绑走的地址会显示成「没绑过」，人重填一次还是失败。
--
-- 和 GetMailAccountByID 选的是同一组列，两边的行类型可以互换。
--
-- **精确比较，不套 lower()。** 地址一律以小写存（00046 把存量也规范化了），
-- 而调用方在服务层已经 ToLower 过。套 lower() 的话有两个坏处：走不上
-- mail_accounts_tenant_id_email_key 那条索引（实测是 Seq Scan），而且和
-- UpsertMailAccountShell 的 ON CONFLICT (tenant_id, email) **口径不一致**
-- ——查的时候匹配上老行、插的时候对不上，同一个信箱会裂成两行，而唯一约束
-- 一声不吭。
SELECT id, employee_id, email, username, auth_kind, verified_at, last_error, auth_failed,
       is_active, is_default, updated_at,
       domain, smtp_host, smtp_port, smtp_security,
       imap_host, imap_port, imap_security
FROM mail_accounts
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND email = sqlc.arg(email)::text;

-- name: GetMailAccountByID :one
-- 按信箱 id 取一个信箱，不含密文。
--
-- 取代了从前的 GetMyMailAccount（按 employee_id 的 :one）。那一句在一人一箱
-- 下没问题，放开之后就是这批改动最怕的形状：pgx 的 QueryRow **读到第一行
-- 就返回、不报「多行」错**，而它没有 ORDER BY——于是「我的邮箱」随机指向
-- 两个箱之一，绿勾、同步故障横幅、reauth 跳哪扇门全都跟着随机。
--
-- 刻意不选 secret_enc：这是设置页读的，凭据永远不回浏览器。
SELECT id, employee_id, email, username, auth_kind, verified_at, last_error, auth_failed,
       is_active, is_default, unbound_at, updated_at,
       domain, smtp_host, smtp_port, smtp_security,
       imap_host, imap_port, imap_security
FROM mail_accounts
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = sqlc.arg(id)::bigint;

-- name: UpsertMailAccountShell :one
-- Creates or updates everything except the secret, and returns the id.
--
-- Split from the secret write because the ciphertext is bound to the row id
-- (see AccountAAD), which does not exist until the row does. Insert first,
-- seal against the real id, then store — rather than inventing the id
-- client-side or binding to something weaker.
--
-- **冲突键是地址，不再是员工。** 00043 删掉了 UNIQUE (tenant_id, employee_id)，
-- 而 ON CONFLICT 必须有支撑索引——不同批改的话，这一句会直接抛 42P10，
-- 绑定邮箱和 Google 回调当场全挂。
--
-- 换成地址之后语义也更贴事实：同一个地址重新填一次授权码是更新，换一个
-- 地址是新增一个信箱。第一个绑的自动成为默认（NOT EXISTS 那一句），之后
-- 加的不动默认——不然每加一个信箱，写信的发件人就被悄悄换掉了。
--
-- **收发服务器从 mail_hosts 种进来。** 00042 把主机从「一家公司一份」搬到
-- 了信箱行上，却漏了这一句：新插的行 smtp_host 是空串，而 ForAccount 见到
-- 空串就返回 ErrMailHostNotConfigured。结果是**新员工绑完邮箱，发信和收信
-- 全停**，一直停到管理员碰巧再去「邮件主机设置」里点一次保存为止（那一下
-- 会触发 SyncAccountHostsFromTenant 把配置刷到所有行）——而这两件事之间
-- 没有任何提示把它们联系起来。
--
-- LEFT JOIN 而不是 JOIN：没配过 mail_hosts 的公司照样要能插进来，只是插出
-- 来的信箱确实还不能收发，而那正是 ErrMailHostNotConfigured 该说的话。
-- coalesce 里的默认值和 00042 加列时的 DEFAULT 一致。
--
-- mail_hosts 从此就是它注释里写的那个角色：**新建信箱时的默认值模板**，
-- 不是发信的事实来源。员工自己挑服务商之后，这里种下的值会被覆盖。
INSERT INTO mail_accounts (
    tenant_id, employee_id, email, username, secret_enc, key_version, is_default,
    domain, smtp_host, smtp_port, smtp_security,
    imap_host, imap_port, imap_security, hourly_quota, daily_quota, updated_at
)
SELECT
    sqlc.arg(tenant_id)::bigint, sqlc.arg(employee_id)::bigint,
    sqlc.arg(email)::text, sqlc.arg(username)::text, ''::bytea, 0,
    NOT EXISTS (
        SELECT 1 FROM mail_accounts d
         WHERE d.tenant_id = sqlc.arg(tenant_id)::bigint
           AND d.employee_id = sqlc.arg(employee_id)::bigint
    ),
    coalesce(h.domain, ''), coalesce(h.smtp_host, ''),
    coalesce(h.smtp_port, 465), coalesce(h.smtp_security, 'SSL'),
    coalesce(h.imap_host, ''),
    coalesce(h.imap_port, 993), coalesce(h.imap_security, 'SSL'),
    coalesce(h.hourly_quota, 100), coalesce(h.daily_quota, 500),
    now()
FROM (SELECT 1) AS seed
LEFT JOIN mail_hosts h ON h.tenant_id = sqlc.arg(tenant_id)::bigint
-- **WHERE 那一行是安全边界，不是优化。** 没有它，DO UPDATE 会把
-- employee_id 改成新来的那个人——也就是说，B 只要知道 A 的邮箱地址，
-- 填一次就能把 A 的信箱连同已同步的全部邮件划到自己名下，一声不吭。
--
-- 加上之后，行属于别人时 DO UPDATE 不匹配，整句既不插也不更，RETURNING
-- 没有行——调用方拿到 ErrNoRows，翻成「这个地址已经被别人绑了」。
ON CONFLICT (tenant_id, email) DO UPDATE SET
    username = excluded.username,
    updated_at = now()
WHERE mail_accounts.employee_id = excluded.employee_id
RETURNING id;

-- name: UpdateMailAccountAddress :exec
-- 把**已有的**这一行改成另一个地址。
--
-- 和 UpsertMailAccountShell 的分工：那一句冲突键是地址，所以"填一个新地址"
-- 会新增一行。设置页上的「改邮箱地址」不是这个意思——它要的是"这一个信箱
-- 换个地址"，在只有一个信箱的年代那两件事看起来一样，现在不一样了。
--
-- 撞上别人已经绑了的地址会违反 UNIQUE (tenant_id, email)，调用方翻译成
-- 人话。
UPDATE mail_accounts
SET email = sqlc.arg(email)::text,
    username = sqlc.arg(username)::text,
    updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: SetMailAccountHosts :exec
-- 把这个信箱的收发服务器写上去。
--
-- 00042 之前主机是一家公司一份（mail_hosts），绑定时不必写——所有箱都用
-- 同一套。跨服务商之后这句是必需的：绑 Gmail 的那一行必须自己带着
-- imap.gmail.com，否则同步会拿着 Gmail 的账号去登公司的 263 服务器，而
-- 那个失败长得和「授权码错了」一模一样。
UPDATE mail_accounts
SET domain = sqlc.arg(domain)::text,
    smtp_host = sqlc.arg(smtp_host)::text,
    smtp_port = sqlc.arg(smtp_port)::int,
    smtp_security = sqlc.arg(smtp_security)::text,
    imap_host = sqlc.arg(imap_host)::text,
    imap_port = sqlc.arg(imap_port)::int,
    imap_security = sqlc.arg(imap_security)::text,
    updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: RecordMailBinding :exec
-- 绑定留痕。见 00044 的表注释——地址交还给调用方之后，「谁绑了什么」不再
-- 有一个不言自明的答案。
--
-- 失败也记：只记成功的话，反复拿别人地址试探正好是看不见的那一半。
INSERT INTO mail_binding_log (
    tenant_id, employee_id, account_id, email, provider, action, detail
) VALUES (
    sqlc.arg(tenant_id)::bigint, sqlc.arg(employee_id)::bigint,
    sqlc.narg(account_id)::bigint, sqlc.arg(email)::text,
    sqlc.arg(provider)::text, sqlc.arg(action)::text, sqlc.arg(detail)::text
);

-- name: ClearDefaultMailbox :exec
-- 换默认信箱的第一步。必须和第二步分成两条语句、放在同一个事务里。
--
-- 一开始写成了带数据修改 CTE 的单句（WITH cleared AS (UPDATE ... FALSE)
-- 后面跟 UPDATE ... TRUE），当场撞 mail_accounts_one_default_idx：Postgres
-- 里数据修改 CTE 和主语句**看的是同一个快照**，主语句看不见 CTE 清掉的那
-- 一行，于是索引在同一条命令里看到两个 TRUE。唯一索引又不能延迟检查
-- （只有约束能 DEFERRABLE，而约束不支持部分索引）。
UPDATE mail_accounts SET is_default = FALSE, updated_at = now()
 WHERE tenant_id = sqlc.arg(tenant_id)::bigint
   AND employee_id = sqlc.arg(employee_id)::bigint
   AND is_default;

-- name: MarkDefaultMailbox :execrows
-- 第二步。返回行数，好让调用方分得清「换好了」和「那个信箱不是他的」。
UPDATE mail_accounts SET is_default = TRUE, updated_at = now()
 WHERE tenant_id = sqlc.arg(tenant_id)::bigint
   AND employee_id = sqlc.arg(employee_id)::bigint
   AND id = sqlc.arg(id)::bigint;

-- name: SetMailAccountSecret :exec
-- Storing a new secret invalidates any previous verification: the code may
-- be wrong, and claiming a mailbox works because an older one did is exactly
-- the kind of stale green tick that stops people trusting the indicator.
-- Typing a code also decides how this account authenticates from now on:
-- auth_kind flips back to PASSWORD and any Google grant is dropped — the
-- mirror image of SetMailAccountOAuth clearing secret_enc.
UPDATE mail_accounts
SET secret_enc = sqlc.arg(secret_enc)::bytea,
    key_version = sqlc.arg(key_version)::int,
    auth_kind = 'PASSWORD',
    oauth_refresh_enc = ''::bytea,
    verified_at = NULL,
    last_error = '',
    -- 重新填一次授权码就是重新绑上：解绑那一刻停掉的收发在这里恢复。
    -- 漏掉这两行的症状是「重新绑了，页面上说绑好了，但信永远不来」——
    -- 因为所有挑信箱的查询都还在按 unbound_at 跳过它。
    is_active = TRUE,
    unbound_at = NULL,
    updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: GetMailAccountSecret :one
-- The only query that returns ciphertext. Used by the sender and the IMAP
-- sync, never by anything that answers an HTTP request.
--
-- **按账号 id 取，不是按员工。** 从前是按员工的，那在「一人一箱」下等价，
-- 而这个前提正要被拿掉：一个人绑了两个箱之后，按员工查是 sqlc 的 :one，
-- 生成 QueryRow，而 pgx 读到第一行就返回、**不报「多行」错误**，这句还
-- 没有 ORDER BY——于是发信和同步会随机挑一个箱，另一个箱一封信都收不到，
-- 日志里一个字都没有。
--
-- 主机配置一并从这一行读（00042 之前它在 mail_hosts 上，一家公司一份，
-- 那是跨服务商真正的拦路虎）。
SELECT id, employee_id, email, username, auth_kind,
       secret_enc, oauth_refresh_enc, key_version, is_active,
       domain, smtp_host, smtp_port, smtp_security,
       imap_host, imap_port, imap_security,
       hourly_quota, daily_quota
FROM mail_accounts
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = sqlc.arg(id)::bigint;

-- name: MarkMailAccountVerified :exec
UPDATE mail_accounts
SET verified_at = now(), last_error = '', updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: MarkMailAccountFailed :exec
-- auth_failed 和 last_error 一起写：文本给人看，位给程序判。
UPDATE mail_accounts
SET last_error = sqlc.arg(last_error)::text,
    auth_failed = sqlc.arg(auth_failed)::boolean,
    updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: SetMailAccountActive :exec
UPDATE mail_accounts
SET is_active = sqlc.arg(is_active)::boolean, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: ListSyncableMailAccounts :many
-- What the IMAP sync walks. Ordered by id so the rotation is stable and one
-- mailbox cannot starve another.
SELECT id, employee_id, email, username, secret_enc, key_version
FROM mail_accounts
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND is_active AND unbound_at IS NULL
ORDER BY id;

-- name: ListMailAccountsForEmployee :many
-- 一个人名下的全部信箱。今天唯一约束保证最多一行，下一期放开之后这里才
-- 会真的返回多行——先把读法改对，免得放开约束那一刻还有地方在按员工取
-- 单行（那会静默挑中随便一个）。
--
-- 和 GetMyMailAccount 一样不选 secret_enc：这是设置页读的，凭据永远不回
-- 浏览器。
SELECT id, email, username, auth_kind, verified_at, last_error, auth_failed, is_active, updated_at,
       is_default, domain, smtp_host, smtp_port, smtp_security,
       imap_host, imap_port, imap_security, last_read_at, unbound_at
FROM mail_accounts
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND employee_id = sqlc.arg(employee_id)::bigint
-- 默认的排在最前：defaultAccountIDFor 取第一行，所以顺序就是「用哪个箱
-- 发信」的答案，不能是随便的 id 序。
ORDER BY is_default DESC, id;

-- name: BumpSendCounter :one
-- Counts one accepted message into the current hour and returns the new
-- total. The caller compares it against the quota; going over is possible
-- only by the width of one message, which is the right trade against holding
-- a lock across a network call to the mail host.
INSERT INTO mail_send_counters (tenant_id, account_id, window_at, sent_count)
VALUES (sqlc.arg(tenant_id)::bigint, sqlc.arg(account_id)::bigint,
        date_trunc('hour', now()), 1)
ON CONFLICT (tenant_id, account_id, window_at)
DO UPDATE SET sent_count = mail_send_counters.sent_count + 1
RETURNING sent_count;

-- name: CountSentInWindow :one
-- Hour and day totals in one round trip, so the pacing check before a send
-- is a single query rather than two.
SELECT
    coalesce(sum(sent_count) FILTER (WHERE window_at >= date_trunc('hour', now())), 0)::int AS this_hour,
    coalesce(sum(sent_count) FILTER (WHERE window_at >= now() - interval '24 hours'), 0)::int AS last_24h
FROM mail_send_counters
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND account_id = sqlc.arg(account_id)::bigint;

-- name: PruneSendCounters :exec
-- Counters older than the widest window we ever ask about are dead weight.
DELETE FROM mail_send_counters
WHERE window_at < now() - interval '7 days';

-- name: GetSyncState :one
SELECT uid_validity, last_uid, low_uid, last_synced_at, last_error
FROM mail_sync_state
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND account_id = sqlc.arg(account_id)::bigint
  AND folder = sqlc.arg(folder)::text;

-- name: UpsertSyncState :exec
-- low_uid only ever moves down: 0 means backfill has not started, so the
-- first write sets it and later writes keep the minimum.
INSERT INTO mail_sync_state (tenant_id, account_id, folder, uid_validity, last_uid, low_uid, last_synced_at, last_error)
VALUES (sqlc.arg(tenant_id)::bigint, sqlc.arg(account_id)::bigint, sqlc.arg(folder)::text,
        sqlc.arg(uid_validity)::bigint, sqlc.arg(last_uid)::bigint, sqlc.arg(low_uid)::bigint, now(), '')
ON CONFLICT (tenant_id, account_id, folder) DO UPDATE SET
    uid_validity = excluded.uid_validity,
    last_uid = excluded.last_uid,
    low_uid = CASE
        WHEN mail_sync_state.low_uid = 0 THEN excluded.low_uid
        WHEN excluded.low_uid = 0 THEN mail_sync_state.low_uid
        ELSE LEAST(mail_sync_state.low_uid, excluded.low_uid)
    END,
    last_synced_at = now(),
    last_error = '';

-- name: MarkSyncFailed :exec
INSERT INTO mail_sync_state (tenant_id, account_id, folder, last_error)
VALUES (sqlc.arg(tenant_id)::bigint, sqlc.arg(account_id)::bigint,
        sqlc.arg(folder)::text, sqlc.arg(last_error)::text)
ON CONFLICT (tenant_id, account_id, folder) DO UPDATE SET
    last_error = excluded.last_error;

-- name: InsertInbound :one
-- ON CONFLICT DO NOTHING plus a returned id of 0 is how a repeated fetch of
-- the same UID becomes a no-op rather than a duplicate row or an error.
INSERT INTO email_inbound (
    tenant_id, account_id, owner_id, folder, imap_uid,
    message_id, in_reply_to, references_ids, thread_key, reply_to_id,
    from_email, from_name, to_email, subject, body_html, body_text, snippet,
    raw_key, raw_size, is_bounce, has_attachments, is_read, sent_at, received_at,
    sent_message_id, search_text,
    customer_id, contact_id, customer_name,
    reply_to, cc, auth_spf, auth_dkim, to_all
) VALUES (
    sqlc.arg(tenant_id)::bigint, sqlc.arg(account_id)::bigint, sqlc.arg(owner_id)::bigint,
    sqlc.arg(folder)::text, sqlc.arg(imap_uid)::bigint,
    sqlc.arg(message_id)::text, sqlc.arg(in_reply_to)::text, sqlc.arg(references_ids)::text,
    sqlc.arg(thread_key)::text, sqlc.narg(reply_to_id)::bigint,
    sqlc.arg(from_email)::text, sqlc.arg(from_name)::text, sqlc.arg(to_email)::text,
    sqlc.arg(subject)::text, sqlc.arg(body_html)::text, sqlc.arg(body_text)::text,
    sqlc.arg(snippet)::text, sqlc.arg(raw_key)::text, sqlc.arg(raw_size)::bigint,
    sqlc.arg(is_bounce)::boolean, sqlc.arg(has_attachments)::boolean,
    sqlc.arg(is_read)::boolean,
    sqlc.narg(sent_at)::timestamptz,
    -- When the mail host says it arrived, not when we happened to fetch it.
    -- Stamping now() here made 收到时间 mean "last time this row was written",
    -- so a resync rewrote every timestamp in the mailbox to the same minute.
    coalesce(sqlc.narg(received_at)::timestamptz, now()),
    -- Non-zero when this is the host's copy of something the ERP sent. The
    -- copy used to be discarded on that basis; keeping it is what gives a
    -- sent mail a message to star, archive or delete.
    sqlc.arg(sent_message_id)::bigint,
    -- The body as plain text. Derived once here rather than at query time:
    -- body_text is empty for HTML-only senders, and body_html cannot be
    -- searched without matching class names and base64. See migration 00023.
    sqlc.arg(search_text)::text,
    -- Non-zero when this mail answers something we sent to a customer.
    sqlc.arg(customer_id)::bigint,
    sqlc.arg(contact_id)::bigint,
    sqlc.arg(customer_name)::text,
    -- 真正的回信地址、抄送，以及收信服务器验过的两个身份。见 00036。
    sqlc.arg(reply_to)::text,
    sqlc.arg(cc)::text,
    sqlc.arg(auth_spf)::text,
    sqlc.arg(auth_dkim)::text,
    -- 整段 To 头。见 00052。
    sqlc.arg(to_all)::text
)
ON CONFLICT (tenant_id, account_id, folder, imap_uid) DO NOTHING
RETURNING id;

-- name: InsertInboundAttachment :exec
INSERT INTO email_inbound_attachments (
    tenant_id, inbound_id, file_name, content_type, file_size, file_key, content_id
) VALUES (
    sqlc.arg(tenant_id)::bigint, sqlc.arg(inbound_id)::bigint, sqlc.arg(file_name)::text,
    sqlc.arg(content_type)::text, sqlc.arg(file_size)::bigint, sqlc.arg(file_key)::text,
    sqlc.arg(content_id)::text
);

-- name: FindMessageByKey :one
-- customer_id and friends come back too: a reply inherits the customer of the
-- message it answers, which is how received mail gets a customer at all
-- without guessing at the sender's address. See migration 00026.
SELECT id, message_key::text AS message_key, thread_key, to_email, campaign_id, sender_id,
       customer_id, contact_id, customer_name
FROM email_messages
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND message_key::text = sqlc.arg(message_key)::text;
-- name: ListInboundThreads :many
-- The same slice of the mailbox as ListInbound, but one row per conversation
-- instead of one row per message — Gmail's list, where "客户回了三次" is one
-- line with a (3) rather than three lines to scan past.
--
-- Grouping happens inside the filtered set, which is what makes a thread
-- appear in exactly the views it belongs to: archive one conversation and it
-- leaves the inbox list whole, rather than the archived message vanishing and
-- its siblings staying behind.
--
-- A message with no thread key is its own conversation ('m:<id>'), so mail
-- that never got a reply is not silently merged with other loose mail.
--
-- **会话按信箱分。** PARTITION BY 带 account_id，和 mail_thread_view 的主键
-- 是同一个口径（00044）。少了它，同一条会话落在两个信箱时，列表里是两行、
-- 一搜索变一行——同一封信在两个屏幕上有两种身份，而且哪一种都不报错。
WITH visible AS (
    SELECT id, account_id, from_email, from_name, subject, snippet, thread_key,
           is_read, is_starred, has_attachments, received_at, sent_at,
           coalesce(nullif(thread_key, ''), 'm:' || id::text) AS group_key,
           received_at AS at
    FROM email_inbound
    WHERE tenant_id = sqlc.arg(tenant_id)::bigint
      AND owner_id = sqlc.arg(owner_id)::bigint
      -- 不传 = 全部信箱。左侧切换器还没上线，前端今天什么都不传。
      AND (sqlc.narg(account_id)::bigint IS NULL
           OR account_id = sqlc.narg(account_id)::bigint)
      AND CASE sqlc.arg(view)::text
            WHEN 'JUNK'  THEN folder = 'JUNK' AND NOT not_junk
            -- The trash holds mail deleted from anywhere, junk included.
            WHEN 'TRASH' THEN folder IN ('INBOX', 'JUNK')
            ELSE (folder = 'INBOX' OR (folder = 'JUNK' AND not_junk))
          END
      AND NOT is_bounce
      AND CASE sqlc.arg(view)::text
            WHEN 'STARRED' THEN is_starred AND deleted_at IS NULL
            WHEN 'ARCHIVE' THEN archived_at IS NOT NULL AND deleted_at IS NULL
            WHEN 'TRASH'   THEN deleted_at IS NOT NULL
            WHEN 'JUNK'    THEN deleted_at IS NULL
            ELSE archived_at IS NULL AND deleted_at IS NULL
          END
      AND (sqlc.arg(keyword)::text = ''
           OR subject ILIKE '%' || sqlc.arg(keyword)::text || '%'
           OR from_email ILIKE '%' || sqlc.arg(keyword)::text || '%'
           OR from_name ILIKE '%' || sqlc.arg(keyword)::text || '%')
), ranked AS (
    SELECT visible.*,
           row_number() OVER (PARTITION BY account_id, group_key ORDER BY at DESC, id DESC) AS rn,
           count(*)          OVER (PARTITION BY account_id, group_key) AS thread_count,
           bool_or(NOT is_read)      OVER (PARTITION BY account_id, group_key) AS any_unread,
           bool_or(is_starred)       OVER (PARTITION BY account_id, group_key) AS any_starred,
           bool_or(has_attachments)  OVER (PARTITION BY account_id, group_key) AS any_attachment
    FROM visible
)
-- The row stands for the whole conversation: the newest message supplies the
-- text and the time, the flags are the conversation's own. Unread if ANY
-- message is unread — a thread with an unanswered question in it must not
-- look handled because the last line happened to be read.
--
-- Keyset, not OFFSET: the page starts strictly after the last row of the
-- previous one, so mail arriving mid-read cannot push a conversation across
-- the page boundary and make it appear twice or not at all, and page 50
-- costs the same as page 2. The price is that there is no jumping to page N
-- — the same trade Gmail makes with its 上一页 / 下一页.
SELECT id, from_email, from_name, subject, snippet, thread_key,
       (NOT any_unread)::boolean   AS is_read,
       any_starred::boolean        AS is_starred,
       any_attachment::boolean     AS has_attachments,
       received_at, sent_at,
       thread_count::int           AS thread_count
FROM ranked
WHERE rn = 1
  -- Row comparison, so ties on the timestamp fall back to the id and no two
  -- conversations can ever occupy the same cursor position.
  AND (sqlc.narg(cursor_at)::timestamptz IS NULL
       OR (at, id) < (sqlc.narg(cursor_at)::timestamptz, sqlc.arg(cursor_id)::bigint))
ORDER BY at DESC, id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: CountInboundThreads :one
-- Conversations, not messages: the pager has to count what the list shows.
--
-- DISTINCT 的是 (信箱, 会话) 这一对，不是会话本身——和上面的 PARTITION BY
-- 同一个口径。只按会话数的话，页码会比列表少，翻到最后一页会缺行。
SELECT count(DISTINCT (account_id, coalesce(nullif(thread_key, ''), 'm:' || id::text)))::bigint
FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND (sqlc.narg(account_id)::bigint IS NULL
       OR account_id = sqlc.narg(account_id)::bigint)
  AND CASE sqlc.arg(view)::text
        WHEN 'JUNK'  THEN folder = 'JUNK' AND NOT not_junk
        -- The trash holds mail deleted from anywhere, junk included.
        WHEN 'TRASH' THEN folder IN ('INBOX', 'JUNK')
        ELSE (folder = 'INBOX' OR (folder = 'JUNK' AND not_junk))
      END
  AND NOT is_bounce
  AND CASE sqlc.arg(view)::text
        WHEN 'STARRED' THEN is_starred AND deleted_at IS NULL
        WHEN 'ARCHIVE' THEN archived_at IS NOT NULL AND deleted_at IS NULL
        WHEN 'TRASH'   THEN deleted_at IS NOT NULL
        WHEN 'JUNK'    THEN deleted_at IS NULL
        ELSE archived_at IS NULL AND deleted_at IS NULL
      END
  AND (sqlc.arg(keyword)::text = ''
       OR subject ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR from_email ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR from_name ILIKE '%' || sqlc.arg(keyword)::text || '%');

-- name: MarkViewRead :many
-- Marks everything the current view shows as read, and nothing else.
--
-- Scoped by the same filters as the list because that is what the button
-- promises: "全部已读" in the junk view must not touch the inbox, and it must
-- never reach into the archive or the trash from either. Every row it changes
-- comes back so the caller can tell the mail host too: reading a mailbox in
-- the ERP has to leave it read in Gmail, one button or one message at a time.
--
-- **这一版还没有按信箱限定**，是有意的一条线。00044 把会话拆成了按信箱，
-- 而这里是「视图级」操作：需要的信箱来自"当前看的是哪个箱"，那个参数要等
-- 左侧切换器那一版才传得进来。
--
-- 为什么可以等：会话级的操作（归档、删除、永久删除）已经在同一批里按信箱
-- 限定了，因为它们的调用方手上有那封信、拿得到 account_id——而那几个弄错
-- 会**吃掉另一个信箱的信**。这里弄错只是"标已读标多了"：可逆，不丢信，
-- 而且用户看得见。TrashJunkView 同理。
UPDATE email_inbound
SET is_read = TRUE
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND NOT is_read
  AND CASE sqlc.arg(view)::text
        WHEN 'JUNK'  THEN folder = 'JUNK' AND NOT not_junk
        -- The trash holds mail deleted from anywhere, junk included.
        WHEN 'TRASH' THEN folder IN ('INBOX', 'JUNK')
        ELSE (folder = 'INBOX' OR (folder = 'JUNK' AND not_junk))
      END
  AND NOT is_bounce
  AND CASE sqlc.arg(view)::text
        WHEN 'STARRED' THEN is_starred AND deleted_at IS NULL
        WHEN 'ARCHIVE' THEN archived_at IS NOT NULL AND deleted_at IS NULL
        WHEN 'TRASH'   THEN deleted_at IS NOT NULL
        WHEN 'JUNK'    THEN deleted_at IS NULL
        ELSE archived_at IS NULL AND deleted_at IS NULL
      END
RETURNING account_id, folder, imap_uid;

-- name: SetThreadFlags :many
-- Housekeeping applied to a whole conversation. Archiving from a page that
-- shows the entire exchange has to move the entire exchange; otherwise the
-- thread stays in the inbox one message lighter, which reads as a bug.
-- Owner-scoped: thread keys are guessable, so this must never reach further
-- than the caller's own mail.
--
-- **也按信箱限定。** 同一条会话可能同时落在 263 和 Gmail 两个信箱里（客户
-- 抄送了两个地址）。不带 account_id 的话，在 263 那边点归档会把 Gmail 那一
-- 份也归档掉，触发器随后把两行都刷新，表里完全自洽——症状是「另一个信箱的
-- 信自己不见了」，没有报错、没有日志。删除也是同一条路，那就不只是不见了。
--
-- 调用方手上一定有 account_id：这条路是从「用户点了某一封信」进来的，
-- MarkInbound 先 GetInbound 拿到那一行，account_id 就在里面。
UPDATE email_inbound
SET is_read    = coalesce(sqlc.narg(read)::boolean, is_read),
    is_starred = coalesce(sqlc.narg(starred)::boolean, is_starred),
    not_junk   = coalesce(sqlc.narg(not_junk)::boolean, not_junk),
    archived_at = CASE
        WHEN sqlc.narg(archived)::boolean IS NULL THEN archived_at
        WHEN sqlc.narg(archived)::boolean THEN coalesce(archived_at, now())
        ELSE NULL END,
    deleted_at = CASE
        WHEN sqlc.narg(deleted)::boolean IS NULL THEN deleted_at
        WHEN sqlc.narg(deleted)::boolean THEN coalesce(deleted_at, now())
        ELSE NULL END
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND account_id = sqlc.arg(account_id)::bigint
  AND thread_key = sqlc.arg(thread_key)::text
  AND thread_key <> ''
RETURNING account_id, folder, imap_uid, is_read, is_starred, message_id, archived_at, deleted_at, not_junk;

-- name: SetInboundFlags :many
-- One statement for all four flags; an absent argument leaves that flag
-- alone. Owner-scoped in the WHERE, so marking somebody else's mail is a
-- no-op rather than a decision.
UPDATE email_inbound
SET is_read    = coalesce(sqlc.narg(read)::boolean, is_read),
    is_starred = coalesce(sqlc.narg(starred)::boolean, is_starred),
    not_junk   = coalesce(sqlc.narg(not_junk)::boolean, not_junk),
    archived_at = CASE
        WHEN sqlc.narg(archived)::boolean IS NULL THEN archived_at
        WHEN sqlc.narg(archived)::boolean THEN coalesce(archived_at, now())
        ELSE NULL END,
    deleted_at = CASE
        WHEN sqlc.narg(deleted)::boolean IS NULL THEN deleted_at
        WHEN sqlc.narg(deleted)::boolean THEN coalesce(deleted_at, now())
        ELSE NULL END
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND id = sqlc.arg(id)::bigint
RETURNING account_id, folder, imap_uid, is_read, is_starred, message_id, archived_at, deleted_at, not_junk;

-- name: GetInbound :one
-- The ERP's delivery record is joined on for the same reason ListSentUnified
-- joins it: 对方是否已读 is knowable only there, and 已发送 opens this row
-- rather than the ERP one whenever the host kept a copy — which, with Gmail,
-- is always. Without the join the answer exists in the database and appears
-- nowhere on the screen.
--
-- LEFT, and null for everything the inbox reads: an inbound mail has no
-- delivery record and must not be made to look like it lost one.
SELECT i.id, i.account_id, i.owner_id, i.message_id, i.thread_key, i.reply_to_id,
       i.from_email, i.from_name, i.to_email, i.subject, i.body_html, i.body_text,
       i.raw_key, i.raw_size, i.is_read, i.has_attachments, i.received_at, i.sent_at,
       i.folder, i.reply_to, i.cc, i.auth_spf, i.auth_dkim, i.to_all,
       coalesce(m.status, '') AS sent_status,
       m.opened_at AS sent_opened_at,
       coalesce(m.tracked, FALSE) AS sent_tracked
FROM email_inbound i
LEFT JOIN email_messages m
       ON m.id = i.sent_message_id AND m.tenant_id = i.tenant_id
WHERE i.tenant_id = sqlc.arg(tenant_id)::bigint AND i.id = sqlc.arg(id)::bigint;

-- name: GetInboundForCompose :one
-- The reply/forward context: the owner (for the caller check), the
-- Message-ID being answered, the chain above it, and the thread this
-- conversation lives in.
--
-- raw_key and subject are here for forward-as-attachment: the original goes
-- out as the stored .eml, named after what the sender called it. raw_key is
-- empty for anything whose MIME never reached object storage, and that has to
-- be refused rather than silently downgraded to a quoted forward — somebody
-- forwarding a mail as evidence needs to know they did not.
SELECT id, owner_id, message_id, references_ids, thread_key, raw_key, subject
FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: MarkInboundRead :many
UPDATE email_inbound SET is_read = TRUE
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND NOT is_read
RETURNING account_id, folder, imap_uid;

-- name: ListInboundAttachments :many
SELECT id, file_name, content_type, file_size, file_key, content_id
FROM email_inbound_attachments
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND inbound_id = sqlc.arg(inbound_id)::bigint
ORDER BY id;

-- name: CountUnread :one
-- The badge counts what the inbox proper shows: archived and trashed mail
-- has been dealt with, so it stops demanding attention.
--
-- **也按信箱算。** 徽标就贴在收件箱那一行上，而列表已经按信箱过滤了——
-- 不带 account_id 的话，切到 A 箱看着五封信，徽标写着 12，那个数字指的是
-- A+B 两个箱。数字和它旁边的列表说的不是一回事，比没有数字更糟。
SELECT count(*)::bigint FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND (sqlc.narg(account_id)::bigint IS NULL
       OR account_id = sqlc.narg(account_id)::bigint)
  AND (folder = 'INBOX' OR (folder = 'JUNK' AND not_junk))
  AND NOT is_bounce AND NOT is_read
  AND archived_at IS NULL AND deleted_at IS NULL;

-- name: CountFolder :one
-- How much of a folder we hold, for the backfill cap.
SELECT count(*)::bigint FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND account_id = sqlc.arg(account_id)::bigint
  AND folder = sqlc.arg(folder)::text;

-- The host's Sent folder alone used to be the answer here. It is not: see
-- ListSentUnified, which merges it with the ERP's own record of what it
-- sent, because each on its own leaves mail out.

-- The Sent folder.
--
-- Every row is a real message in the host's Sent folder, which is what makes
-- starring, archiving and deleting work here exactly as they do in the inbox:
-- there is a message, in a folder, with a UID to write back to. The ERP's
-- delivery record is joined on for the two things only it knows — per-
-- recipient status, and whether the tracking pixel was ever fetched.
--
-- They are matched by sent_message_id, stamped at ingest from the message_key
-- the ERP wrote into the Message-ID. Exact, not a guess: an earlier version
-- of this matched on recipient + subject + a ten-minute window, which was
-- only ever needed because the copy was being thrown away.
--
-- The second half is the safety net. A host is not obliged to keep a copy of
-- what it relayed — Gmail does, 263 is unverified — and a mail that was sent,
-- accepted and delivered must never be missing from 已发送 because of that.
-- So an accepted send with no copy still appears, but only after a grace
-- period: within it, the copy is simply on its way and showing a second,
-- weaker row for the same mail would be noise. In normal operation nobody
-- ever sees one of these.

-- name: ListSentUnified :many
-- **两条腿一起按信箱筛。**
--
-- 它合并两个来源：email_inbound 里 folder='SENT' 的那些（邮件服务器自己存
-- 的副本），和 email_messages 里我们发出去而服务器没留副本的那些。
--
-- 前一条一直有 account_id；后一条是 00047 才加上的——在那之前「这封信从
-- 哪个信箱发出去的」根本答不上来，出站队列只记 sender_id。那时**只给一半
-- 加筛选会更糟**：切到 Gmail 箱，看到的是 Gmail 的已发送 + 全部的 ERP
-- 发送记录，一半对一半不对而且看不出哪一半。所以当时两条都不筛。
--
-- 现在两条腿都有了。不传就是全部——旧前端和「一个箱都没绑」的人走这条。
WITH host AS (
    SELECT 'HOST'::text AS kind, i.id, i.to_email, coalesce(m.to_name, '') AS to_name,
           i.subject, i.snippet,
           coalesce(i.sent_at, i.received_at) AS at,
           coalesce(m.status, '') AS status,
           m.opened_at,
           coalesce(m.tracked, FALSE) AS tracked,
           i.has_attachments, i.is_starred, i.thread_key,
           i.raw_size,
           -- 整段收件人（00052）。列表那一列从前只写第一个，详情却列全部，
           -- 同一封信两个地方两个说法。
           i.to_all
    FROM email_inbound i
    LEFT JOIN email_messages m
           ON m.id = i.sent_message_id AND m.tenant_id = i.tenant_id
    WHERE i.tenant_id = sqlc.arg(tenant_id)::bigint
      AND i.owner_id = sqlc.arg(owner_id)::bigint
      AND i.folder = 'SENT'
      AND (sqlc.narg(account_id)::bigint IS NULL
           OR i.account_id = sqlc.narg(account_id)::bigint)
      AND i.deleted_at IS NULL
      AND i.archived_at IS NULL
), orphan AS (
    SELECT 'ERP'::text AS kind, m.id, m.to_email, m.to_name, m.subject,
           left(coalesce(nullif(m.body_text, ''), CASE WHEN m.body_format = 'HTML' THEN '' ELSE m.body END), 200) AS snippet,
           m.sent_at AS at,
           m.status, m.opened_at, m.tracked,
           EXISTS (SELECT 1 FROM email_attachments a
                   WHERE a.tenant_id = m.tenant_id AND a.campaign_id = m.campaign_id) AS has_attachments,
           FALSE AS is_starred,
           '' AS thread_key,
           -- 投递记录没有原件，也就没有大小。按大小排时它们沉在最底下，
           -- 而不是拿正文长度冒充一个数。
           0::bigint AS raw_size,
           ''::text AS to_all
    FROM email_messages m
    WHERE m.tenant_id = sqlc.arg(tenant_id)::bigint
      AND m.sender_id = sqlc.arg(owner_id)::bigint
      AND m.sent_at IS NOT NULL
      -- 00047 之前入队的行 account_id 是 0，那些只在「不筛」时出现。
      -- 把它们塞进任何一个箱都是猜的，而猜错的样子是「这封信不是我从这个
      -- 地址发的」——比少一行更难解释。
      AND (sqlc.narg(account_id)::bigint IS NULL
           OR m.account_id = sqlc.narg(account_id)::bigint)
      AND m.sent_at < now() - interval '10 minutes'
      AND NOT EXISTS (
          SELECT 1 FROM email_inbound i
          WHERE i.tenant_id = m.tenant_id AND i.sent_message_id = m.id
      )
      -- …and only if it left from the mailbox being read. Rebinding a mailbox
      -- deletes its synced messages but not the ERP's delivery records, so
      -- without this every send from a previous binding reappears here as an
      -- orphan — permanently, since its copy was in a mailbox nobody is
      -- signed in to any more.
      AND EXISTS (
          SELECT 1 FROM mail_accounts b
          WHERE b.tenant_id = m.tenant_id AND b.employee_id = m.sender_id
            AND CASE WHEN m.from_email <> ''
                     -- Stamped at send time: compare the address itself.
                     THEN lower(m.from_email) = lower(b.email)
                     -- Sent before migration 00021, so the address was never
                     -- recorded. The Message-ID still is, and its domain was
                     -- built from the sending address — evidence, not a guess.
                     -- It cannot separate two mailboxes at one domain, which
                     -- is the residual cost of not having stamped it earlier
                     -- and is why the column now exists.
                     ELSE rtrim(split_part(m.provider_id, '@', 2), '>')
                          = split_part(b.email, '@', 2)
                END
      )
)
SELECT kind, id, to_email, to_name, subject, snippet, at, status, opened_at,
       tracked, has_attachments, is_starred, thread_key, raw_size, to_all, sort_key
FROM (
    -- 排序键统一成一段文本，理由见 ListThreadsByViewSorted。日期那一档是
    -- 默认，也是从前唯一的一档：UTC 的 20 位数字串，字典序即时间序。
    SELECT u.*,
           (CASE sqlc.arg(sort_by)::text
              WHEN 'to'      THEN lower(u.to_email)
              WHEN 'subject' THEN lower(u.subject)
              WHEN 'size'    THEN lpad(u.raw_size::text, 20, '0')
              ELSE to_char(u.at AT TIME ZONE 'UTC', 'YYYYMMDDHH24MISSUS')
            END)::text AS sort_key
    FROM (SELECT * FROM host UNION ALL SELECT * FROM orphan) u
) s
WHERE (sqlc.arg(keyword)::text = ''
       OR subject ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR to_email ILIKE '%' || sqlc.arg(keyword)::text || '%')
  -- Keyset, like every other mailbox list. kind joins the sort key because
  -- the two halves number their rows independently, so (sort_key, id) alone
  -- is not a unique position.
  AND (sqlc.narg(cursor_key)::text IS NULL
       OR CASE WHEN sqlc.arg(sort_dir)::text = 'asc'
               THEN (sort_key, kind, id) > (sqlc.narg(cursor_key)::text,
                                            sqlc.arg(cursor_kind)::text,
                                            sqlc.arg(cursor_id)::bigint)
               ELSE (sort_key, kind, id) < (sqlc.narg(cursor_key)::text,
                                            sqlc.arg(cursor_kind)::text,
                                            sqlc.arg(cursor_id)::bigint)
          END)
ORDER BY CASE WHEN sqlc.arg(sort_dir)::text = 'asc' THEN sort_key END ASC,
         CASE WHEN sqlc.arg(sort_dir)::text = 'asc' THEN kind END ASC,
         CASE WHEN sqlc.arg(sort_dir)::text = 'asc' THEN id END ASC,
         sort_key DESC, kind DESC, id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: CountSentUnified :one
-- Repeats the shape rather than sharing it, because the pager has to agree
-- with the list: a count that skipped the grace period would promise rows
-- that are not there.
WITH host AS (
    SELECT i.subject, i.to_email
    FROM email_inbound i
    WHERE i.tenant_id = sqlc.arg(tenant_id)::bigint
      AND i.owner_id = sqlc.arg(owner_id)::bigint
      AND i.folder = 'SENT'
      AND (sqlc.narg(account_id)::bigint IS NULL
           OR i.account_id = sqlc.narg(account_id)::bigint)
      AND i.deleted_at IS NULL
      AND i.archived_at IS NULL
), orphan AS (
    SELECT m.subject, m.to_email
    FROM email_messages m
    WHERE m.tenant_id = sqlc.arg(tenant_id)::bigint
      AND m.sender_id = sqlc.arg(owner_id)::bigint
      AND m.sent_at IS NOT NULL
      -- 00047 之前入队的行 account_id 是 0，那些只在「不筛」时出现。
      -- 把它们塞进任何一个箱都是猜的，而猜错的样子是「这封信不是我从这个
      -- 地址发的」——比少一行更难解释。
      AND (sqlc.narg(account_id)::bigint IS NULL
           OR m.account_id = sqlc.narg(account_id)::bigint)
      AND m.sent_at < now() - interval '10 minutes'
      AND NOT EXISTS (
          SELECT 1 FROM email_inbound i
          WHERE i.tenant_id = m.tenant_id AND i.sent_message_id = m.id
      )
      -- …and only if it left from the mailbox being read. Rebinding a mailbox
      -- deletes its synced messages but not the ERP's delivery records, so
      -- without this every send from a previous binding reappears here as an
      -- orphan — permanently, since its copy was in a mailbox nobody is
      -- signed in to any more.
      AND EXISTS (
          SELECT 1 FROM mail_accounts b
          WHERE b.tenant_id = m.tenant_id AND b.employee_id = m.sender_id
            AND CASE WHEN m.from_email <> ''
                     -- Stamped at send time: compare the address itself.
                     THEN lower(m.from_email) = lower(b.email)
                     -- Sent before migration 00021, so the address was never
                     -- recorded. The Message-ID still is, and its domain was
                     -- built from the sending address — evidence, not a guess.
                     -- It cannot separate two mailboxes at one domain, which
                     -- is the residual cost of not having stamped it earlier
                     -- and is why the column now exists.
                     ELSE rtrim(split_part(m.provider_id, '@', 2), '>')
                          = split_part(b.email, '@', 2)
                END
      )
)
SELECT count(*)::bigint FROM (SELECT * FROM host UNION ALL SELECT * FROM orphan) u
WHERE (sqlc.arg(keyword)::text = ''
       OR subject ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR to_email ILIKE '%' || sqlc.arg(keyword)::text || '%');

-- name: ListThread :many
-- Both sides of one conversation, in the order they happened. Sent and
-- received come from different tables, so the union is what makes a thread
-- read as a dialogue instead of two separate lists. Owner-scoped on both
-- legs: a thread key is guessable, whose mail it opens must not be.
--
-- **收到的那一腿还按信箱限定**（00044 的口径）：列表行上的 (2) 说的是
-- 「这个信箱里的两封」，打开时不限定信箱就会把两个箱的同名会话合起来读，
-- 变成列表写 (2)、进去 4 封。
--
-- 发出去的那一腿（email_messages）**限定不了**：那张表还没有 account_id，
-- 「这封信从哪个信箱发的」今天答不上来，那是第三期的事。所以现在的语义是
-- 「这个信箱收到的 + 我发出去的全部」。会话在两个信箱里时，发出去的那几封
-- 两边都会出现——比把收到的也合起来要好，因为那几封确实是同一批。
SELECT 'OUT' AS direction, m.id, m.subject, m.body, m.body_format,
       m.to_email AS counterparty, m.sender_name AS who,
       coalesce(m.sent_at, m.queued_at) AS at
FROM email_messages m
WHERE m.tenant_id = sqlc.arg(tenant_id)::bigint
  AND m.sender_id = sqlc.arg(owner_id)::bigint
  AND m.thread_key = sqlc.arg(thread_key)::text
UNION ALL
SELECT 'IN' AS direction, i.id, i.subject,
       CASE WHEN i.body_html <> '' THEN i.body_html ELSE i.body_text END AS body,
       CASE WHEN i.body_html <> '' THEN 'HTML' ELSE 'TEXT' END AS body_format,
       i.from_email AS counterparty, i.from_name AS who,
       coalesce(i.sent_at, i.received_at) AS at
FROM email_inbound i
WHERE i.tenant_id = sqlc.arg(tenant_id)::bigint
  AND i.owner_id = sqlc.arg(owner_id)::bigint
  -- 不传 = 这个人名下所有信箱（旧前端）。见 GetMailThreadRequest.message_id。
  AND (sqlc.narg(account_id)::bigint IS NULL
       OR i.account_id = sqlc.narg(account_id)::bigint)
  AND i.thread_key = sqlc.arg(thread_key)::text
  AND NOT i.is_bounce
  -- A mail speaks once per conversation. Gmail files a copy of every send
  -- into the SENT folder, and the host mirror syncs that copy in as one more
  -- inbound row — the same physical mail under a second id. Unguarded, a
  -- reply sent from the ERP appears twice (its OUT row and its mirror), and
  -- a self-addressed mail twice (its INBOX copy and its SENT copy). A SENT
  -- copy is therefore silenced when the mail it duplicates is already in the
  -- thread; one without a Message-ID cannot be proven a duplicate and stays.
  AND NOT (i.folder = 'SENT' AND i.message_id <> '' AND (
    -- the mirror of a mail this service sent: its Message-ID was minted
    -- from the outbound row's message_key
    EXISTS (
      SELECT 1 FROM email_messages sent
      WHERE sent.tenant_id = i.tenant_id AND sent.sender_id = i.owner_id
        AND sent.message_key::text = split_part(i.message_id, '@', 1)
    )
    -- the SENT copy of a mail another folder already shows (a mail sent to
    -- yourself from any client: the INBOX copy is the one that stays)
    OR EXISTS (
      SELECT 1 FROM email_inbound twin
      WHERE twin.tenant_id = i.tenant_id AND twin.owner_id = i.owner_id
        AND twin.thread_key = i.thread_key AND twin.message_id = i.message_id
        AND twin.id <> i.id AND twin.folder <> 'SENT' AND NOT twin.is_bounce
    )
  ))
ORDER BY at;

-- name: FindMessageByKeyAnyTenant :one
-- The tracking pixel is fetched by a recipient's mail client, which carries
-- no session and therefore no tenant. The key is a random UUID, so it is the
-- only identifier available — and knowing one tells you nothing beyond the
-- message it belongs to.
--
-- sent_at comes back because the gap between sending and the first fetch is
-- one of the few things that separates a person from a scanner: a security
-- gateway loads the image while the message is still in transit, and nobody
-- reads their mail within seconds of it landing.
SELECT id, tenant_id, to_email, sent_at FROM email_messages
WHERE message_key::text = sqlc.arg(message_key)::text;

-- name: MarkOpened :exec
-- First open only. A later fetch appends an event but must not overwrite the
-- original timestamp: "when did they first look at it" is the useful figure,
-- and Apple Mail's prefetching would otherwise keep moving it forward.
UPDATE email_messages
SET opened_at = coalesce(opened_at, now())
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: ListSentWithEngagement :many
-- The sent list with what little we know about what happened next.
--
-- reply_count comes from real inbound messages threaded onto this send, which
-- is the only unambiguous evidence a person read it. opened_at comes from the
-- tracking pixel and is presented as a maybe.
SELECT m.id, m.message_key::text AS message_key, m.subject, m.to_email, m.to_name,
       m.status, m.thread_key, m.sent_at, m.queued_at, m.opened_at,
       (SELECT count(*) FROM email_inbound i
         WHERE i.tenant_id = m.tenant_id AND i.thread_key = m.thread_key
           AND NOT i.is_bounce)::int AS reply_count
FROM email_messages m
WHERE m.tenant_id = sqlc.arg(tenant_id)::bigint
  AND m.sender_id = sqlc.arg(sender_id)::bigint
-- id 收口：群发一次 50 封，queued_at 全落在同一毫秒，是这几个列表里最容易
-- 打平的一个。打平 + OFFSET 分页 = 翻页时行会重复或漏掉。
ORDER BY coalesce(m.sent_at, m.queued_at) DESC, m.id DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: SetMailAccountOAuth :exec
-- Binding via Google replaces whatever was there: the password ciphertext is
-- cleared because keeping a stale second credential around only widens what a
-- leak could do. Verified immediately — the owner literally just signed in.
UPDATE mail_accounts
SET auth_kind = 'OAUTH',
    oauth_refresh_enc = sqlc.arg(oauth_refresh_enc)::bytea,
    secret_enc = ''::bytea,
    key_version = sqlc.arg(key_version)::int,
    verified_at = now(),
    last_error = '',
    is_active = TRUE,
    -- 同 SetMailAccountSecret：重新授权就是重新绑上。
    unbound_at = NULL,
    updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: DeleteInboundForAccount :exec
-- Rebinding to a different mailbox makes every stored message and UID
-- meaningless; attachments go with their messages via the cascade.
DELETE FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND account_id = sqlc.arg(account_id)::bigint;

-- name: DeleteSyncStateForAccount :exec
DELETE FROM mail_sync_state
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND account_id = sqlc.arg(account_id)::bigint;

-- name: GetInboundForPurge :one
-- Only a mail already in the trash qualifies: permanent deletion is a second
-- step after a soft delete, never a first action on a live mail.
SELECT id, raw_key, account_id, folder, imap_uid, message_id
FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND deleted_at IS NOT NULL;

-- name: ListThreadForPurge :many
-- Every trashed message of one conversation. Permanent deletion follows the
-- same conversation semantics as the rest of the list: the trash row stands
-- for the exchange, so confirming deletes the exchange. Only trashed rows —
-- a live message of the same thread is not swept up by this.
--
-- **按信箱限定**（00044 的口径）。这一句后面接的是**永久删除**加一条发给
-- 邮件服务器的删除指令，所以「同一条会话也落在另一个信箱里」的那份必须
-- 留下。调用方手上有 account_id：进这条路之前先 GetInbound 拿了那一行。
SELECT id, raw_key, account_id, folder, imap_uid, message_id
FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND account_id = sqlc.arg(account_id)::bigint
  AND thread_key = sqlc.arg(thread_key)::text
  AND thread_key <> ''
  AND deleted_at IS NOT NULL
ORDER BY id;

-- name: PurgeInbound :execrows
-- The attachment rows go with the mail via ON DELETE CASCADE; their object
-- storage copies are removed by the caller before this runs.
DELETE FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND deleted_at IS NOT NULL;

-- name: EnqueueFlagOp :exec
-- The intent to publish one flag change. Conflicting intents collapse: the
-- newest wins, because that is the state the person last chose.
INSERT INTO mail_flag_ops (tenant_id, account_id, employee_id, folder, imap_uid, flag, op, message_id)
VALUES (
    sqlc.arg(tenant_id)::bigint, sqlc.arg(account_id)::bigint,
    sqlc.arg(employee_id)::bigint,
    sqlc.arg(folder)::text, sqlc.arg(imap_uid)::bigint,
    sqlc.arg(flag)::text, sqlc.arg(op)::text, sqlc.arg(message_id)::text
)
ON CONFLICT (tenant_id, account_id, folder, imap_uid, flag) DO UPDATE SET
    op = excluded.op,
    message_id = excluded.message_id,
    attempts = 0,
    last_error = '',
    next_try_at = now();

-- name: ClaimFlagOps :many
-- Due work, oldest first, locked so two workers cannot publish the same
-- change twice. SKIP LOCKED rather than waiting: another worker holding a row
-- means it is already being handled.
SELECT id, tenant_id, account_id, employee_id, folder, imap_uid, flag, op, message_id, attempts
FROM mail_flag_ops
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND next_try_at <= now()
ORDER BY next_try_at
LIMIT sqlc.arg(row_limit)::int
FOR UPDATE SKIP LOCKED;

-- name: DeleteFlagOp :exec
DELETE FROM mail_flag_ops WHERE id = sqlc.arg(id)::bigint;

-- name: FailFlagOp :exec
-- Backs off so a mailbox that is refusing connections is retried at a
-- widening interval rather than hammered every cycle.
UPDATE mail_flag_ops
SET attempts = attempts + 1,
    last_error = sqlc.arg(last_error)::text,
    next_try_at = now() + (least(attempts + 1, 6) * interval '2 minutes')
WHERE id = sqlc.arg(id)::bigint;

-- name: CountPendingFlagOps :one
SELECT count(*)::bigint FROM mail_flag_ops
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND account_id = sqlc.arg(account_id)::bigint;

-- name: ListRecentUIDs :many
-- The newest slice of one folder, for reconciling flags against the host.
-- Bounded: re-reading a whole mailbox every cycle would cost more than the
-- disagreement it is looking for.
SELECT imap_uid, is_read, is_starred
FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND account_id = sqlc.arg(account_id)::bigint
  AND folder = sqlc.arg(folder)::text
ORDER BY imap_uid DESC
LIMIT sqlc.arg(row_limit)::int;

-- Stars used to be taken one UID at a time here, alongside the read state.
-- SyncStarredFromHost replaced that: the host can name every starred message
-- in a folder in one search, so there is nothing left for a per-message
-- version to do.

-- name: SetInboundReadByUID :exec
-- Server state winning over ours, for one message. Used only by the
-- reconcile pass, and only once the write-back queue is empty for this
-- account — otherwise it would overwrite a local change still on its way up.
UPDATE email_inbound
SET is_read = sqlc.arg(is_read)::boolean
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND account_id = sqlc.arg(account_id)::bigint
  AND folder = sqlc.arg(folder)::text
  AND imap_uid = sqlc.arg(imap_uid)::bigint;

-- name: ListTrashForPurge :many
-- Everything in one person's trash, for emptying it in one go.
--
-- **和列表的信箱筛选是一对，谁先加谁就得把另一个带上。** 今天两边都是
-- 「我全部信箱」，所以「清空回收站」清掉的正是屏幕上列着的那些——一致。
-- 左侧切换器一上线，列表变成「只看这个信箱」，这一句要是没跟着变，
-- 按钮上写着"清空回收站"，清掉的却是另一个信箱里也在回收站的信，而那是
-- **永久删除**，还会连带发一条删除指令给邮件服务器。
--
-- 同一对的还有 MarkViewRead 和 TrashJunkView（那两个可逆，这一个不可逆）。
SELECT id, raw_key, account_id, folder, imap_uid, message_id
FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND deleted_at IS NOT NULL
ORDER BY id;

-- name: ListExpiredTrash :many
-- Trash old enough to clear out by itself. Tenant-wide and owner-agnostic
-- because the sweeper runs for everybody at once; the owner comes back on
-- each row so the delete stays owner-scoped like every other one.
SELECT id, owner_id, raw_key, account_id, folder, imap_uid, message_id
FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND deleted_at IS NOT NULL
  AND deleted_at < sqlc.arg(cutoff)::timestamptz
ORDER BY id
LIMIT sqlc.arg(row_limit)::int;

-- name: RepointInbound :exec
-- Follows a message the ERP itself moved on the host: same mail, new folder,
-- new UID. Without this the row would still name a UID that belongs to
-- nothing, and the next sync would fetch the message again as though it were
-- newly arrived — one mail, two rows.
UPDATE email_inbound
SET folder = sqlc.arg(new_folder)::text,
    imap_uid = sqlc.arg(new_uid)::bigint,
    not_junk = FALSE
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND account_id = sqlc.arg(account_id)::bigint
  AND folder = sqlc.arg(old_folder)::text
  AND imap_uid = sqlc.arg(old_uid)::bigint;

-- name: ListRecentForReconcile :many
-- The newest slice of one folder with everything the reconcile pass needs to
-- decide what happened to each message.
--
-- owner_id and raw_key are here for the one outcome that destroys something:
-- a message already in our recycle bin that the host has now purged is purged
-- here too, and that means removing its objects before its row.
SELECT id, owner_id, imap_uid, message_id, raw_key, is_read, is_starred, archived_at, deleted_at
FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND account_id = sqlc.arg(account_id)::bigint
  AND folder = sqlc.arg(folder)::text
ORDER BY imap_uid DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: SyncStarredFromHost :execrows
-- Makes the ERP's stars agree with the host's, over the whole folder at once.
--
-- Stars used to ride along with the read-state reconcile, which fetches flags
-- for the newest 200 UIDs. That window is a few days of a busy mailbox, so a
-- star put on anything older in Gmail simply never arrived — the symptom was
-- a mailbox full of stars showing exactly one in the ERP.
--
-- A single UID SEARCH FLAGGED answers the question for the entire folder in
-- one round trip, which is why this can be a plain assignment rather than a
-- per-message comparison: starred is "in the set the host just named".
--
-- Rescued junk is left alone. Those rows still carry their old JUNK folder and
-- UID while the message itself has moved to the host's inbox, so the search
-- would not name them and this would quietly unstar them.
--
-- The final predicate keeps the update to rows that actually change, so a
-- mailbox with no star activity costs nothing every two minutes.
UPDATE email_inbound
SET is_starred = (imap_uid = ANY(sqlc.arg(starred_uids)::bigint[]))
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND account_id = sqlc.arg(account_id)::bigint
  AND folder = sqlc.arg(folder)::text
  AND NOT not_junk
  AND is_starred <> (imap_uid = ANY(sqlc.arg(starred_uids)::bigint[]));

-- name: MirrorHostDelete :exec
-- Somebody deleted this mail elsewhere. Mirrored as a soft delete, never a
-- hard one: our copy may be the only one left, and the ERP trash gives thirty
-- days to notice a mistake. The sweeper finishes the job afterwards.
UPDATE email_inbound
SET deleted_at = coalesce(deleted_at, now())
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND account_id = sqlc.arg(account_id)::bigint
  AND folder = sqlc.arg(folder)::text
  AND imap_uid = sqlc.arg(imap_uid)::bigint;

-- name: MirrorHostArchive :exec
UPDATE email_inbound
SET archived_at = coalesce(archived_at, now())
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND account_id = sqlc.arg(account_id)::bigint
  AND folder = sqlc.arg(folder)::text
  AND imap_uid = sqlc.arg(imap_uid)::bigint;

-- name: TrashJunkView :many
-- Empties the junk view into the trash in one go.
--
-- Scoped exactly like the junk list: a mail somebody has already rescued with
-- 「这不是垃圾」 is not junk any more and must not be swept up with the rest.
-- Every touched row comes back so the deletion can be carried to the host too,
-- the same as deleting one by hand.
UPDATE email_inbound
SET deleted_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND folder = 'JUNK'
  AND NOT not_junk
  AND deleted_at IS NULL
RETURNING id, account_id, folder, imap_uid, message_id;

-- Search, across folders.
--
-- Every other list here answers "what is in this folder". This one answers
-- "where is that mail", which is a different question: somebody who remembers
-- a phrase does not remember whether they filed it, and making them guess the
-- folder before they can look is making them do the search themselves.
--
-- Junk and trash are left out, the way Gmail leaves them out. Both are full
-- of things the person already decided against, and a search that surfaces
-- them puts rejected mail beside wanted mail with no way to tell which is
-- which. A mail rescued from junk (not_junk) is a decision the other way and
-- is included.
--
-- name: SearchMail :many
WITH hits AS (
    SELECT id, folder, thread_key, from_email, from_name, to_email, subject,
           snippet, search_text, is_read, is_starred, has_attachments,
           received_at, sent_at
    FROM email_inbound
    WHERE tenant_id = sqlc.arg(tenant_id)::bigint
      AND owner_id = sqlc.arg(owner_id)::bigint
      -- 只搜这个箱。不传 = 全部，留给旧令牌和一个箱都没绑的人。
      AND (sqlc.narg(account_id)::bigint IS NULL
           OR account_id = sqlc.narg(account_id)::bigint)
      AND deleted_at IS NULL
      AND (folder <> 'JUNK' OR not_junk)
      -- One column, not five ORed together. The subject and the addresses
      -- are folded into search_text at ingest precisely so this can be a
      -- single predicate: an OR across columns cannot use the trigram index
      -- and the planner falls back to a scan — 100 ms against 1.6 ms,
      -- measured on this mailbox.
      AND search_text ILIKE '%' || sqlc.arg(keyword)::text || '%'
      AND (sqlc.narg(cursor_at)::timestamptz IS NULL
           OR (received_at, id) < (sqlc.narg(cursor_at)::timestamptz,
                                   sqlc.arg(cursor_id)::bigint))
    ORDER BY received_at DESC, id DESC
    LIMIT sqlc.arg(row_limit)::int
)
-- The match window is cut here, after LIMIT, so lowering a whole mail body to
-- find the offset happens for the fifty rows on screen and not for every row
-- the scan touched.
SELECT id, folder, thread_key, from_email, from_name, to_email, subject,
       is_read, is_starred, has_attachments, received_at, sent_at,
       CASE
           WHEN position(lower(sqlc.arg(keyword)::text) in lower(search_text)) > 0
           THEN substring(search_text
                    -- A little before the hit, so the phrase has context on
                    -- both sides instead of starting mid-word at the match.
                    from greatest(1, position(lower(sqlc.arg(keyword)::text) in lower(search_text)) - 40)
                    for 200)
           -- The hit was in the subject or an address, which the row already
           -- shows. Falling back to the opening line is more use than an
           -- empty space where a quotation would go.
           ELSE snippet
       END::text AS match_snippet
FROM hits
ORDER BY received_at DESC, id DESC;

-- name: CountSearchMail :one
-- Repeats the predicate rather than sharing it: the count and the list have
-- to agree, and a count that searched a different set would promise rows that
-- are not there.
SELECT count(*)::bigint
FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND (sqlc.narg(account_id)::bigint IS NULL
       OR account_id = sqlc.narg(account_id)::bigint)
  AND deleted_at IS NULL
  AND (folder <> 'JUNK' OR not_junk)
  AND search_text ILIKE '%' || sqlc.arg(keyword)::text || '%';

-- name: ListCustomerMail :many
WITH ours AS (
    SELECT 'OUT'::text AS direction, m.id, m.subject,
           left(coalesce(nullif(m.body_text, ''), ''), 200) AS snippet,
           m.to_email AS counterparty, m.sent_at AS at
    FROM email_messages m
    WHERE m.tenant_id = sqlc.arg(tenant_id)::bigint
      AND m.customer_id = sqlc.arg(customer_id)::bigint
      AND m.sent_at IS NOT NULL
), theirs AS (
    SELECT 'IN'::text AS direction, i.id, i.subject, i.snippet,
           i.from_email AS counterparty, i.received_at AS at
    FROM email_inbound i
    WHERE i.tenant_id = sqlc.arg(tenant_id)::bigint
      AND i.customer_id = sqlc.arg(customer_id)::bigint
      AND i.deleted_at IS NULL
)
SELECT direction, id, subject, snippet, counterparty, at
FROM (SELECT * FROM ours UNION ALL SELECT * FROM theirs) conversation
ORDER BY at DESC, id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: ListThreadsByView :many
-- The mailbox list, read from the rows it shows.
--
-- Replaces a CTE that materialised every message the owner could see, sorted
-- it by conversation, ran four window functions over it and took the newest
-- twenty-five off the end. That work was proportional to the whole mailbox on
-- every page load - 70 ms and a disk-spilling sort for a heavy user - because
-- a conversation's count and unread badge are facts about all of its messages
-- and no index can compute them.
--
-- mail_thread_view holds those facts already, maintained by trigger. See
-- migration 00034.
--
-- Keyset, not OFFSET: the page starts strictly after the last row of the
-- previous one, so mail arriving mid-read cannot push a conversation across a
-- page boundary and make it appear twice or not at all, and page 50 costs the
-- same as page 2.
SELECT m.id, m.from_email, m.from_name, m.subject, m.snippet, m.thread_key,
       (NOT t.any_unread)::boolean     AS is_read,
       t.any_starred::boolean          AS is_starred,
       t.any_attachment::boolean       AS has_attachments,
       m.received_at, m.sent_at,
       t.msg_count::int                AS thread_count
FROM mail_thread_view t
JOIN email_inbound m ON m.tenant_id = t.tenant_id AND m.id = t.last_id
WHERE t.tenant_id = sqlc.arg(tenant_id)::bigint
  AND t.owner_id = sqlc.arg(owner_id)::bigint
  -- 不传 = 全部信箱，走 00034 那条旧索引；传了走
  -- mail_thread_view_account_list_idx（00044）。左侧切换器上线前，前端
  -- 什么都不传，所以两条索引这一版都还要在。
  AND (sqlc.narg(account_id)::bigint IS NULL
       OR t.account_id = sqlc.narg(account_id)::bigint)
  AND t.view = sqlc.arg(view)::text
  -- Row comparison, so ties on the timestamp fall back to the id and no two
  -- conversations can ever occupy the same cursor position.
  AND (sqlc.narg(cursor_at)::timestamptz IS NULL
       OR (t.last_at, t.last_id) < (sqlc.narg(cursor_at)::timestamptz,
                                    sqlc.arg(cursor_id)::bigint))
ORDER BY t.last_at DESC, t.last_id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: ListThreadsByViewSorted :many
-- 同一份列表，按人点的那一列排。
--
-- 和 ListThreadsByView 分成两条而不是合成一条：上面那条靠索引顺序直接读出
-- 前二十五行，是一天到晚都在走的路；这一条要把整个箱的会话拿出来排一遍，
-- 只在有人点了排序栏时才走。把 CASE 塞进上面那条的 ORDER BY 会让索引顺序
-- 用不上，等于为一个偶尔用的功能给常走的路加税。
--
-- **排序键统一成一段文本。** 发件人和主题本来就是文本；大小补零到 20 位，
-- 时间格式化成 UTC 的 20 位数字串——两者按字典序比就是按数值比。这样做的
-- 好处是游标只有一种形状：(sort_key, id)，不用给每一列各写一套「上一页
-- 停在哪」的比较。lower() 是为了让大小写不同的同一个名字排在一起，而不是
-- 看数据库的排序规则脸色。
--
-- 会话的「发件人」「主题」「大小」都是**最后一封**的——列表上那一行显示的
-- 就是它，排的也是它。一条会话里有十封信，大小按最后那封算。
--
-- 方向靠两组 CASE：asc 时前两个键生效、后两个键在完全相同的 (sort_key, id)
-- 上才轮得到（不可能相同，所以无害）；desc 时前两个键全是 NULL，等价于
-- 只按后两个排。
SELECT x.id, x.from_email, x.from_name, x.subject, x.snippet, x.thread_key,
       x.is_read, x.is_starred, x.has_attachments, x.received_at, x.sent_at,
       x.thread_count, x.raw_size, x.sort_key
FROM (
    SELECT m.id, m.from_email, m.from_name, m.subject, m.snippet, m.thread_key,
           (NOT t.any_unread)::boolean     AS is_read,
           t.any_starred::boolean          AS is_starred,
           t.any_attachment::boolean       AS has_attachments,
           m.received_at, m.sent_at,
           t.msg_count::int                AS thread_count,
           m.raw_size,
           (CASE sqlc.arg(sort_by)::text
              WHEN 'from'    THEN lower(coalesce(nullif(m.from_name, ''), m.from_email))
              WHEN 'subject' THEN lower(m.subject)
              WHEN 'size'    THEN lpad(m.raw_size::text, 20, '0')
              ELSE to_char(t.last_at AT TIME ZONE 'UTC', 'YYYYMMDDHH24MISSUS')
            END)::text                    AS sort_key
    FROM mail_thread_view t
    JOIN email_inbound m ON m.tenant_id = t.tenant_id AND m.id = t.last_id
    WHERE t.tenant_id = sqlc.arg(tenant_id)::bigint
      AND t.owner_id = sqlc.arg(owner_id)::bigint
      AND (sqlc.narg(account_id)::bigint IS NULL
           OR t.account_id = sqlc.narg(account_id)::bigint)
      AND t.view = sqlc.arg(view)::text
) x
-- 上一页停在哪：asc 往大了走，desc 往小了走。id 兜底，两条会话不可能占同一个位置。
WHERE (sqlc.narg(cursor_key)::text IS NULL
       OR CASE WHEN sqlc.arg(sort_dir)::text = 'asc'
               THEN (x.sort_key, x.id) > (sqlc.narg(cursor_key)::text, sqlc.arg(cursor_id)::bigint)
               ELSE (x.sort_key, x.id) < (sqlc.narg(cursor_key)::text, sqlc.arg(cursor_id)::bigint)
          END)
ORDER BY CASE WHEN sqlc.arg(sort_dir)::text = 'asc' THEN x.sort_key END ASC,
         CASE WHEN sqlc.arg(sort_dir)::text = 'asc' THEN x.id END ASC,
         x.sort_key DESC, x.id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: CountThreadsByView :one
-- Conversations, not messages: the pager has to count what the list shows.
SELECT count(*)::bigint FROM mail_thread_view
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND (sqlc.narg(account_id)::bigint IS NULL
       OR account_id = sqlc.narg(account_id)::bigint)
  AND view = sqlc.arg(view)::text;

-- name: ListThreadAttachments :many
-- 整条会话的附件，一次取回，两个方向。
--
-- 按会话取而不是逐封取：一段十六轮的往来会变成十六次往返，而这些行加起来
-- 也就几十条。分组交给 Go。
--
-- content_id 一并取回，但**不在这里过滤**。
--
-- 曾经这里写的是 a.content_id = ''，理由是「内嵌图片不该列成附件」。那个判断
-- 是错的：Gmail 给每一个 MIME 部件都写 Content-ID，包括真附件。结果是一封
-- 带简历的信，附件在库里好好存着，界面上一个都不显示。
--
-- 「是不是内嵌」的正确判断只有一个：**正文有没有真的引用那个 cid**。那件事
-- 需要正文，所以留给 Go 里的 hideEmbedded 做——GetInbound 一直是这么做的，
-- 这里当初不该另发明一个更粗的代理指标。
SELECT 'IN'::text AS direction, i.id AS message_id,
       a.id, a.file_name, a.content_type, a.file_size, a.file_key, a.content_id
FROM email_inbound i
JOIN email_inbound_attachments a
  ON a.tenant_id = i.tenant_id AND a.inbound_id = i.id
WHERE i.tenant_id = sqlc.arg(tenant_id)::bigint
  AND i.owner_id = sqlc.arg(owner_id)::bigint
  -- 和 ListThread 同一个口径：附件跟着信走，信按信箱分。
  AND (sqlc.narg(account_id)::bigint IS NULL
       OR i.account_id = sqlc.narg(account_id)::bigint)
  AND i.thread_key = sqlc.arg(thread_key)::text
UNION ALL
SELECT 'OUT'::text AS direction, m.id AS message_id,
       a.id, a.file_name, a.content_type, a.file_size, a.file_key, ''::text AS content_id
FROM email_messages m
JOIN email_attachments a
  ON a.tenant_id = m.tenant_id AND a.campaign_id = m.campaign_id
WHERE m.tenant_id = sqlc.arg(tenant_id)::bigint
  AND m.sender_id = sqlc.arg(owner_id)::bigint
  AND m.thread_key = sqlc.arg(thread_key)::text
ORDER BY 1, 2, 3;

-- name: ListTenantsWithMailboxes :many
-- 后台三个循环（收信轮询、IDLE 长连接、发信 worker）要服务的公司名单。
--
-- 从 mail_accounts 推导，而不是去 iam 服务问「有哪些公司」：这个服务需要的不是
-- 「所有公司」，是「有邮箱要处理的公司」，而那个集合就在本库里。少一次跨服务
-- 调用，也少一份会过期的名单。
--
-- 每轮重新查，不缓存。缓存的名单会让新开的公司要等到进程重启才被发现——而
-- Odoo 的 cron 在多库部署下的不稳定，正是从一份「不断变化却被当成稳定」的库
-- 名单开始的。这条查询是主键范围内的一次扫描，比那个风险便宜得多。
--
-- 也刻意不做「按公司开关」：Frappe 每个站点要单独 enable-scheduler，最常见的
-- 故障就是有人忘了开，然后邮件安静地堆在队列里没人发。绑了邮箱就该被服务，
-- 不该再有第二个开关。
SELECT DISTINCT tenant_id FROM mail_accounts WHERE is_active AND unbound_at IS NULL ORDER BY tenant_id;

-- name: GetInboundByFolderUID :one
-- 挪信收尾（repoint）先问一句：目的位置是不是已经被人占了。占位的几乎总是
-- 同一封信——IDLE 推送让同步抢在收尾之前把挪过去的信当新邮件下载了一遍。
SELECT id, owner_id, raw_key, message_id
FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND account_id = sqlc.arg(account_id)::bigint
  AND folder = sqlc.arg(folder)::text
  AND imap_uid = sqlc.arg(imap_uid)::bigint;

-- name: CountUnreadByMailbox :many
-- 这个人**每个信箱**各有多少封没读，一次问完。
--
-- 左侧那排角标读的就是它。一个箱一次 CountUnread 的话，绑三个箱就是三次
-- 往返，而这三个数字总是一起显示的。
--
-- 口径必须和 CountUnread 一模一样，否则切过去之后角标和列表对不上——
-- 那比没有角标更糟：数字说有 3 封，点进去一封都没有，人会以为信丢了。
-- 归档过、删掉的、退信、以及垃圾箱里没被捞回来的都不算。
--
-- 只回有未读的箱。一个都没有的箱不出现在结果里，调用方按 0 处理——
-- 让 SQL 回一堆 0 再让调用方过滤，两边都要记住这件事。
SELECT account_id, count(*)::bigint AS unread
FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND (folder = 'INBOX' OR (folder = 'JUNK' AND not_junk))
  AND NOT is_bounce AND NOT is_read
  AND archived_at IS NULL AND deleted_at IS NULL
GROUP BY account_id;

-- name: TouchMailboxRead :exec
-- 有人在看这个箱。
--
-- **限了频**：邮件页每次翻页、每次开信都会打到这条路径，逐次写等于把一次读
-- 变成一次写。半分钟内只落一次——这个值是拿来分档的（有人看的全量同步、
-- 没人看的只刷未读数），半分钟的粒度绰绰有余。
--
-- 条件写进 WHERE 而不是先读后判：并发的两个请求都读到「该写了」再各写一次，
-- 是这类计数最常见的写法，也是最常见的浪费。
UPDATE mail_accounts
   SET last_read_at = now()
 WHERE tenant_id = sqlc.arg(tenant_id)::bigint
   AND id = sqlc.arg(id)::bigint
   AND (last_read_at IS NULL OR last_read_at < now() - interval '30 seconds');

-- name: ListActiveMailAccounts :many
-- 这一轮要**全量同步**的信箱：最近有人在看的那些。
--
-- 「有人在看」= last_read_at 新鲜（00048 在收件箱那条路上写的）。它是人的
-- 动作，不是我们自己的动作——后者在 mail_sync_state 上，每轮都会更新，拿它
-- 分档等于所有箱永远都算活跃。
--
-- 从没被看过的箱（last_read_at IS NULL）算不活跃。刚部署时所有行都是空的，
-- 那一段时间里所有箱都走轻状态那条路——**这不影响对错**：轻状态看见新信
-- 就把那个箱提上来全量同步，代价只是最长十分钟的延迟。人一打开邮箱页，
-- last_read_at 就写上了，下一轮它就回到全量这一档。
SELECT id, employee_id, email, username, secret_enc, key_version
FROM mail_accounts
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND is_active
  -- 解绑了的箱没有凭据，登不上去。这里不挡的话，每轮都会为它跑一次注定
  -- 失败的登录，然后把「认证失败」写到那一行上——而那句话在一个主动解绑
  -- 的人看来毫无道理。
  AND unbound_at IS NULL
  AND last_read_at IS NOT NULL
  AND last_read_at > now() - make_interval(secs => sqlc.arg(active_seconds)::int)
ORDER BY id;

-- name: ListMailboxesDueForStatus :many
-- 这一轮要问**轻状态**的信箱：没人在看，而且离上次问过够久了。
--
-- 和上面那条互斥：有人在看的箱已经在全量同步，再问一次 STATUS 是白问。
--
-- 靠「离上次够久」自然错开，不是攒一批一起问：600 个箱一起问会在一轮里堆出
-- 一个尖峰，把全量那一档挤掉。每轮只有到点的那些进来，摊平之后大约是总数
-- 的十分之一。
--
-- NULLS FIRST：从没问过的排最前。刚部署、以及刚绑好的箱属于这一类，它们
-- 最需要先被看一眼。
SELECT id, employee_id, email, username, secret_enc, key_version
FROM mail_accounts
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND is_active
  AND unbound_at IS NULL
  AND (last_read_at IS NULL
       OR last_read_at <= now() - make_interval(secs => sqlc.arg(active_seconds)::int))
  AND (status_checked_at IS NULL
       OR status_checked_at <= now() - make_interval(secs => sqlc.arg(status_seconds)::int))
ORDER BY status_checked_at NULLS FIRST, id
LIMIT sqlc.arg(row_limit)::int;

-- name: MarkStatusChecked :exec
-- 问过了。时间戳只在这里写，所以「到点没」这件事只有一个来源。
UPDATE mail_accounts
   SET status_checked_at = now()
 WHERE tenant_id = sqlc.arg(tenant_id)::bigint
   AND id = sqlc.arg(id)::bigint;

-- name: HighestSyncedUID :one
-- 我们已经取到这个信箱这个文件夹的哪一封了。
--
-- 拿它和服务器回的 UIDNEXT 比：UIDNEXT 超过 last_uid + 1，就说明有我们
-- 没取过的信。**这是个精确的比较**，不是估计——UID 在一个 UIDVALIDITY 里
-- 单调递增，这正是它存在的意义。
--
-- 没有这一行（从没同步过）回 0，于是任何 UIDNEXT 都算「有新的」，那也正是
-- 一个新绑的箱该有的待遇。
SELECT coalesce(max(last_uid), 0)::bigint
FROM mail_sync_state
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND account_id = sqlc.arg(account_id)::bigint
  AND folder = sqlc.arg(folder)::text;

-- name: MailboxIsBeingRead :one
-- 这个信箱此刻算不算「有人在看」。
--
-- 常开连接（IDLE）那一档用它决定要不要继续守着。守着一个没人看的箱，代价
-- 是一条一直占着的 IMAP 连接和一个 goroutine——300 人一人两箱就是 600 条，
-- 而其中大部分箱当天根本没人打开过。
--
-- IDLE 的用处是「让**正在看**的那个收件箱像是活的」。没人看的时候，两分钟
-- 一轮的轮询加十分钟一次的轻状态已经够了。
SELECT (last_read_at IS NOT NULL
        AND last_read_at > now() - make_interval(secs => sqlc.arg(active_seconds)::int)
        AND unbound_at IS NULL)::bool
FROM mail_accounts
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: UnbindMailAccount :execrows
-- 解绑：断连接，留历史。
--
-- 清掉的是**凭据**——密码密文和 Google 的 refresh token 一起清，不留任何
-- 一把还能登上去的钥匙。verified_at 也清掉：那个绿勾说的是「这个箱现在能
-- 用」，而它现在不能。
--
-- 不动的是 email、email_inbound、附件、对象存储里的原始 MIME。左栏那一行
-- 还在，点进去照样读得到历史。
--
-- WHERE 带 employee_id：只能解自己的。不是他的就影响零行，调用方翻成 404，
-- 而不是「解成功了」——后者会让一个探测 id 的人拿到「这个 id 存在」。
--
-- 已经解过的再解一次影响零行（unbound_at IS NULL），所以重复点不会把时间戳
-- 往后推——留痕里那个「什么时候解的」要是第一次。
UPDATE mail_accounts
   SET secret_enc = ''::bytea,
       oauth_refresh_enc = ''::bytea,
       key_version = 0,
       verified_at = NULL,
       last_error = '',
       is_active = FALSE,
       is_default = FALSE,
       unbound_at = now(),
       updated_at = now()
 WHERE tenant_id = sqlc.arg(tenant_id)::bigint
   AND id = sqlc.arg(id)::bigint
   AND employee_id = sqlc.arg(employee_id)::bigint
   AND unbound_at IS NULL;

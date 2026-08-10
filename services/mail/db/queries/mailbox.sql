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

-- name: GetMyMailAccount :one
-- Deliberately does NOT select secret_enc. This is what the settings page
-- reads, and a credential that is never returned to a browser cannot be
-- leaked by one.
SELECT id, email, username, auth_kind, verified_at, last_error, is_active, updated_at
FROM mail_accounts
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND employee_id = sqlc.arg(employee_id)::bigint;

-- name: UpsertMailAccountShell :one
-- Creates or updates everything except the secret, and returns the id.
--
-- Split from the secret write because the ciphertext is bound to the row id
-- (see AccountAAD), which does not exist until the row does. Insert first,
-- seal against the real id, then store — rather than inventing the id
-- client-side or binding to something weaker.
INSERT INTO mail_accounts (
    tenant_id, employee_id, email, username, secret_enc, key_version, updated_at
) VALUES (
    sqlc.arg(tenant_id)::bigint, sqlc.arg(employee_id)::bigint,
    sqlc.arg(email)::text, sqlc.arg(username)::text, ''::bytea, 0, now()
)
ON CONFLICT (tenant_id, employee_id) DO UPDATE SET
    email = excluded.email,
    username = excluded.username,
    updated_at = now()
RETURNING id;

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
    updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: GetMailAccountSecret :one
-- The only query that returns ciphertext. Used by the sender and the IMAP
-- sync, never by anything that answers an HTTP request.
SELECT id, email, username, auth_kind, secret_enc, oauth_refresh_enc, key_version, is_active
FROM mail_accounts
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND employee_id = sqlc.arg(employee_id)::bigint;

-- name: MarkMailAccountVerified :exec
UPDATE mail_accounts
SET verified_at = now(), last_error = '', updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: MarkMailAccountFailed :exec
UPDATE mail_accounts
SET last_error = sqlc.arg(last_error)::text, updated_at = now()
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
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND is_active
ORDER BY id;

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
    customer_id, contact_id, customer_name
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
    sqlc.arg(customer_name)::text
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

-- name: ListInbound :many
-- A bounce is machinery, not correspondence, so it is kept out of the inbox
-- and surfaced through the needs-attention queue instead.
-- The view decides which slice of the mailbox this is: the inbox proper
-- (not archived, not trashed), starred (wherever it lives, except trash),
-- the archive, or the trash.
--
-- Every view but the trash is scoped to the inbox — junk is its own place and
-- only rejoins the mailbox once somebody rescues it. The trash is the one
-- exception, and has to be: mail deleted out of the junk folder is still
-- deleted mail. Scoping the trash the same way as the rest left it invisible
-- — soft-deleted in the database, absent from every screen, and plainly
-- sitting in the host's own trash, which reads as the ERP having lost it.
-- Emptying the trash purged those rows regardless, so the count above the
-- list and the number the button deleted disagreed.
SELECT id, from_email, from_name, subject, snippet, thread_key,
       is_read, is_starred, has_attachments, received_at, sent_at
FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
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
ORDER BY coalesce(sent_at, received_at) DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: CountInbound :one
SELECT count(*)::bigint FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
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
WITH visible AS (
    SELECT id, from_email, from_name, subject, snippet, thread_key,
           is_read, is_starred, has_attachments, received_at, sent_at,
           coalesce(nullif(thread_key, ''), 'm:' || id::text) AS group_key,
           received_at AS at
    FROM email_inbound
    WHERE tenant_id = sqlc.arg(tenant_id)::bigint
      AND owner_id = sqlc.arg(owner_id)::bigint
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
           row_number() OVER (PARTITION BY group_key ORDER BY at DESC, id DESC) AS rn,
           count(*)          OVER (PARTITION BY group_key) AS thread_count,
           bool_or(NOT is_read)      OVER (PARTITION BY group_key) AS any_unread,
           bool_or(is_starred)       OVER (PARTITION BY group_key) AS any_starred,
           bool_or(has_attachments)  OVER (PARTITION BY group_key) AS any_attachment
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
SELECT count(DISTINCT coalesce(nullif(thread_key, ''), 'm:' || id::text))::bigint
FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
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
SELECT id, account_id, owner_id, message_id, thread_key, reply_to_id,
       from_email, from_name, to_email, subject, body_html, body_text,
       raw_key, raw_size, is_read, has_attachments, received_at, sent_at
FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

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
SELECT count(*)::bigint FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
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
WITH host AS (
    SELECT 'HOST'::text AS kind, i.id, i.to_email, coalesce(m.to_name, '') AS to_name,
           i.subject, i.snippet,
           coalesce(i.sent_at, i.received_at) AS at,
           coalesce(m.status, '') AS status,
           m.opened_at,
           coalesce(m.tracked, FALSE) AS tracked,
           i.has_attachments, i.is_starred, i.thread_key
    FROM email_inbound i
    LEFT JOIN email_messages m
           ON m.id = i.sent_message_id AND m.tenant_id = i.tenant_id
    WHERE i.tenant_id = sqlc.arg(tenant_id)::bigint
      AND i.owner_id = sqlc.arg(owner_id)::bigint
      AND i.folder = 'SENT'
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
           '' AS thread_key
    FROM email_messages m
    WHERE m.tenant_id = sqlc.arg(tenant_id)::bigint
      AND m.sender_id = sqlc.arg(owner_id)::bigint
      AND m.sent_at IS NOT NULL
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
       tracked, has_attachments, is_starred, thread_key
FROM (SELECT * FROM host UNION ALL SELECT * FROM orphan) u
WHERE (sqlc.arg(keyword)::text = ''
       OR subject ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR to_email ILIKE '%' || sqlc.arg(keyword)::text || '%')
  -- Keyset, like every other mailbox list. kind joins the sort key because
  -- the two halves number their rows independently, so (at, id) alone is not
  -- a unique position.
  AND (sqlc.narg(cursor_at)::timestamptz IS NULL
       OR (at, kind, id) < (sqlc.narg(cursor_at)::timestamptz,
                            sqlc.arg(cursor_kind)::text,
                            sqlc.arg(cursor_id)::bigint))
ORDER BY at DESC, kind DESC, id DESC
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
      AND i.deleted_at IS NULL
      AND i.archived_at IS NULL
), orphan AS (
    SELECT m.subject, m.to_email
    FROM email_messages m
    WHERE m.tenant_id = sqlc.arg(tenant_id)::bigint
      AND m.sender_id = sqlc.arg(owner_id)::bigint
      AND m.sent_at IS NOT NULL
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
  AND i.thread_key = sqlc.arg(thread_key)::text
  AND NOT i.is_bounce
ORDER BY at;

-- name: FindMessageByKeyAnyTenant :one
-- The tracking pixel is fetched by a recipient's mail client, which carries
-- no session and therefore no tenant. The key is a random UUID, so it is the
-- only identifier available — and knowing one tells you nothing beyond the
-- message it belongs to.
SELECT id, tenant_id, to_email FROM email_messages
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
ORDER BY coalesce(m.sent_at, m.queued_at) DESC
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
SELECT id, raw_key, account_id, folder, imap_uid, message_id
FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
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
  AND deleted_at IS NULL
  AND (folder <> 'JUNK' OR not_junk)
  AND search_text ILIKE '%' || sqlc.arg(keyword)::text || '%';

-- name: ListInboundNeedingSearchText :many
-- Rows stored before the column existed. Bounded per call so the backfill
-- runs in batches instead of loading every body at once.
SELECT id, subject, from_name, from_email, to_email, body_text, body_html
FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND search_text = ''
  AND (body_text <> '' OR body_html <> '')
ORDER BY id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: SetSearchText :exec
UPDATE email_inbound
SET search_text = sqlc.arg(search_text)::text
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- Everything said to and by one customer, newest first.
--
-- The question the whole link exists to answer. Both directions in one list:
-- what we sent lives in email_messages, what came back in email_inbound, and
-- a person asking "what have we said to ACME" means both halves — a list of
-- only our own side would read as if the customer never answered.
--
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

-- name: ListInboundWithUnresolvedCID :many
-- Messages whose body points at a part by Content-ID that no stored row
-- satisfies.
--
-- These are not a curiosity: until 2026-08-10 the parser only kept a part that
-- carried a filename, and an image pasted into Gmail's composer carries none —
-- only a Content-ID. Those parts were read past and dropped, so the body was
-- left citing something that does not exist and the reader drew an empty box.
--
-- The raw message is required, because recovery means parsing it again; a row
-- whose original was never stored cannot be helped and is left out rather than
-- returned for ever.
SELECT i.id, i.raw_key, i.account_id
FROM email_inbound i
WHERE i.tenant_id = sqlc.arg(tenant_id)::bigint
  AND i.raw_key <> ''
  AND i.body_html LIKE '%cid:%'
  AND EXISTS (
      SELECT 1 FROM regexp_matches(i.body_html, 'cid:([^"'']+)', 'g') AS m(cid)
      WHERE NOT EXISTS (
          SELECT 1 FROM email_inbound_attachments a
          WHERE a.inbound_id = i.id AND a.content_id = m.cid[1]
      )
  )
ORDER BY i.id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: CountInboundAttachmentWithCID :one
SELECT count(*) FROM email_inbound_attachments
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND inbound_id = sqlc.arg(inbound_id)::bigint
  AND content_id = sqlc.arg(content_id)::text;

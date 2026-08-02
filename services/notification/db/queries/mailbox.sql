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
    raw_key, raw_size, is_bounce, has_attachments, is_read, sent_at
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
    sqlc.narg(sent_at)::timestamptz
)
ON CONFLICT (tenant_id, account_id, folder, imap_uid) DO NOTHING
RETURNING id;

-- name: InsertInboundAttachment :exec
INSERT INTO email_inbound_attachments (
    tenant_id, inbound_id, file_name, content_type, file_size, file_key
) VALUES (
    sqlc.arg(tenant_id)::bigint, sqlc.arg(inbound_id)::bigint, sqlc.arg(file_name)::text,
    sqlc.arg(content_type)::text, sqlc.arg(file_size)::bigint, sqlc.arg(file_key)::text
);

-- name: FindMessageByKey :one
SELECT id, message_key::text AS message_key, thread_key, to_email, campaign_id, sender_id
FROM email_messages
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND message_key::text = sqlc.arg(message_key)::text;

-- name: ListInbound :many
-- A bounce is machinery, not correspondence, so it is kept out of the inbox
-- and surfaced through the needs-attention queue instead.
-- The view decides which slice of the mailbox this is: the inbox proper
-- (not archived, not trashed), starred (wherever it lives, except trash),
-- the archive, or the trash.
SELECT id, from_email, from_name, subject, snippet, thread_key,
       is_read, is_starred, has_attachments, received_at, sent_at
FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND CASE WHEN sqlc.arg(view)::text = 'JUNK'
        THEN folder = 'JUNK' AND NOT not_junk
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
  AND CASE WHEN sqlc.arg(view)::text = 'JUNK'
        THEN folder = 'JUNK' AND NOT not_junk
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

-- name: SetInboundFlags :exec
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
  AND id = sqlc.arg(id)::bigint;

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
SELECT id, owner_id, message_id, references_ids, thread_key
FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: MarkInboundRead :exec
UPDATE email_inbound SET is_read = TRUE
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND id = sqlc.arg(id)::bigint;

-- name: ListInboundAttachments :many
SELECT id, file_name, content_type, file_size, file_key
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

-- name: ListMailboxSent :many
-- Mail sent from the mailbox itself — through any client, over the whole
-- history the backfill has reached. ERP sends live in email_messages with
-- per-recipient status; these are plain copies from the host's Sent folder.
SELECT id, from_email, from_name, to_email, subject, snippet, thread_key,
       has_attachments, received_at, sent_at
FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND folder = 'SENT'
  AND (sqlc.arg(keyword)::text = ''
       OR subject ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR to_email ILIKE '%' || sqlc.arg(keyword)::text || '%')
ORDER BY coalesce(sent_at, received_at) DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: CountMailboxSent :one
SELECT count(*)::bigint FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND folder = 'SENT'
  AND (sqlc.arg(keyword)::text = ''
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
SELECT id, raw_key
FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND deleted_at IS NOT NULL;

-- name: PurgeInbound :execrows
-- The attachment rows go with the mail via ON DELETE CASCADE; their object
-- storage copies are removed by the caller before this runs.
DELETE FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND deleted_at IS NOT NULL;

-- Caching the pictures a received mail points at.

-- name: ListInboundNeedingImages :many
-- The work queue for the caching pass. Newest first.
--
-- It ran oldest-first at first, on the reasoning that a mailbox should fill in
-- the order it arrived. That was wrong twice over, and measurably so: with
-- 1573 of 2625 messages done, the twenty most recent had **none**. A new
-- arrival gets the highest id, so oldest-first puts every incoming mail at the
-- back of a thousand-message queue — the pass spends its afternoon on 2024
-- while the mail somebody is about to open is the last thing it will reach.
--
-- Newest first fixes both cases with the same ordering: mail that has just
-- arrived is at the head of the queue and is cached within a pass, and the
-- backfill walks history backwards, which is the order people read it in.
--
-- Only messages with an HTML body can reference a remote picture, but the
-- pass stamps everything it looks at either way — a text-only mail that kept
-- coming back would spin the loop for ever.
SELECT id, body_html
FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND images_cached_at IS NULL
ORDER BY id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: MarkImagesCached :exec
-- Stamped whatever the outcome. A message whose pictures could not be fetched
-- has still been looked at, and retrying it on every pass would mean a dead
-- host holds up the whole queue.
UPDATE email_inbound
SET images_cached_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: InsertInboundImage :exec
-- ON CONFLICT DO NOTHING: the same message can be passed over twice after a
-- restart, and the second run must not fail on a picture already stored.
INSERT INTO email_inbound_images (
    tenant_id, inbound_id, source_url, url_hash, object_key, content_type, byte_size
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(inbound_id)::bigint,
    sqlc.arg(source_url)::text,
    sqlc.arg(url_hash)::bytea,
    sqlc.arg(object_key)::text,
    sqlc.arg(content_type)::text,
    sqlc.arg(byte_size)::bigint
)
ON CONFLICT (tenant_id, inbound_id, url_hash) DO NOTHING;

-- name: ListInboundImages :many
-- What the read path substitutes into one message's body.
SELECT source_url, object_key, content_type
FROM email_inbound_images
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND inbound_id = sqlc.arg(inbound_id)::bigint;

-- name: ListThreadImages :many
-- The same for a whole conversation, in one query rather than one per turn.
-- A sixteen-message thread would otherwise be sixteen round trips before the
-- first byte reaches the browser.
SELECT g.inbound_id, g.source_url, g.object_key, g.content_type
FROM email_inbound_images g
JOIN email_inbound i ON i.id = g.inbound_id AND i.tenant_id = g.tenant_id
WHERE g.tenant_id = sqlc.arg(tenant_id)::bigint
  AND i.owner_id = sqlc.arg(owner_id)::bigint
  AND i.thread_key = sqlc.arg(thread_key)::text;

-- name: ListImageKeysForPurge :many
-- The object keys behind one message, read before the row goes. The cascade
-- removes the rows; nothing removes the bytes unless we name them first.
SELECT object_key FROM email_inbound_images
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND inbound_id = sqlc.arg(inbound_id)::bigint
  AND object_key <> '';

-- ------------------------------------------- pictures carried in the message

-- name: ListInboundEmbedded :many
-- The parts this message points at from its own body, for the swap that turns
-- every cid: into a signed storage URL.
SELECT id, content_id, file_key, content_type
FROM email_inbound_attachments
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND inbound_id = sqlc.arg(inbound_id)::bigint
  AND content_id <> ''
  AND file_key <> '';

-- name: ListThreadEmbedded :many
-- The same across a whole conversation, in one query rather than one per turn.
SELECT a.inbound_id, a.id, a.content_id, a.file_key, a.content_type
FROM email_inbound_attachments a
JOIN email_inbound i ON i.id = a.inbound_id AND i.tenant_id = a.tenant_id
WHERE a.tenant_id = sqlc.arg(tenant_id)::bigint
  AND i.owner_id = sqlc.arg(owner_id)::bigint
  AND i.thread_key = sqlc.arg(thread_key)::text
  AND a.content_id <> ''
  AND a.file_key <> '';

-- name: ListInboundNeedingContentIDs :many
-- Messages whose body points at a part none of their attachments answers to.
--
-- The work queue for the backfill, and it needs no marker column: a message
-- qualifies exactly while it is still broken, so fixing one removes it from
-- the queue. Bodies written before ingest kept Content-ID are the whole of the
-- backlog, and there is no way for a new message to join it.
--
-- raw_key is what makes the repair possible at all: the original MIME is
-- still in object storage, which is what it is kept for.
SELECT i.id, i.raw_key
FROM email_inbound i
WHERE i.tenant_id = sqlc.arg(tenant_id)::bigint
  AND i.raw_key <> ''
  AND i.body_html ~ 'src="cid:|src=''cid:'
  AND NOT EXISTS (
      SELECT 1 FROM email_inbound_attachments a
      WHERE a.tenant_id = i.tenant_id AND a.inbound_id = i.id AND a.content_id <> ''
  )
ORDER BY i.id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: SetAttachmentContentID :exec
UPDATE email_inbound_attachments
SET content_id = sqlc.arg(content_id)::text
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: RestoreInboundBody :execrows
UPDATE email_inbound
SET body_html = sqlc.arg(body_html)::text,
    body_text = sqlc.arg(body_text)::text,
    snippet   = sqlc.arg(snippet)::varchar,
    has_attachments = sqlc.arg(has_attachments)::boolean
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: DeleteInboundBodyMisfiledAsAttachment :execrows
-- The two rows the old rule wrote for a body it mistook for files. Narrow on
-- purpose: text/* only, and only for a message being repaired, so a genuinely
-- attached .txt on some other message is never touched.
DELETE FROM email_inbound_attachments
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND inbound_id = sqlc.arg(inbound_id)::bigint
  AND (content_type LIKE 'text/plain%' OR content_type LIKE 'text/html%');

-- name: ListInboundMissingRaw :many
-- Rows whose original was lost to the raw-key collision and might still be
-- on the mail host. Only rows carrying a Message-ID qualify: it is the
-- identifier the refetch searches by, and the one proof that what comes back
-- is this message rather than whatever inherited the UID since. The stored
-- imap_uid is deliberately not selected — trusting a UID across generations
-- is the mistake that lost these originals in the first place.
--
-- 按 account_id 分组，不是 owner_id：重取要连回**这封信当初进来的那个
-- 信箱**。一个人绑了两个箱之后，按人分组会拿着 A 箱的凭据去 B 箱上搜
-- Message-ID，搜不到就把行判成"对方删了"。
SELECT id, account_id, owner_id, folder, message_id
FROM email_inbound
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND raw_key = ''
  AND message_id <> ''
ORDER BY account_id, folder, id;

-- name: AdoptInboundRawKey :execrows
-- Claims a re-fetched original, but only for a row still missing one: a key
-- written by anything else in the meantime is not this pass's to overwrite.
UPDATE email_inbound
SET raw_key = sqlc.arg(raw_key)::varchar, raw_size = sqlc.arg(raw_size)::bigint
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND raw_key = '';

-- name: AttachmentsByKeys :many
-- The attachments behind a set of storage keys, scoped to one person's own mail.
--
-- Used when an outgoing mail quotes a picture that arrived on an earlier turn.
-- The key is read out of the body, and the body is not a source of authority:
-- an <img src> is something a person can type. So the key is a *question* asked
-- here, and only a key that comes back is ever opened. Without this, "inline
-- whatever the body points at" would be an instruction from outside to read an
-- arbitrary object out of storage.
--
-- Scoped to the sender's own mail rather than to the tenant, because that is
-- the body they could legitimately have built: the composer quotes a message
-- they can already read.
SELECT a.file_key, a.content_type
FROM email_inbound_attachments a
JOIN email_inbound i ON i.id = a.inbound_id AND i.tenant_id = a.tenant_id
WHERE a.tenant_id = sqlc.arg(tenant_id)::bigint
  AND i.owner_id = sqlc.arg(owner_id)::bigint
  AND a.file_key = ANY(sqlc.arg(file_keys)::text[]);

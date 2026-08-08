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

-- Exporting a conversation, and the record of having done it.

-- name: ListThreadForExport :many
-- One conversation, both directions, oldest first — the same union as
-- ListThread and owner-scoped for the same reason, but carrying the sender's
-- own plain-text alternative alongside the HTML.
--
-- ListThread prefers the HTML because it is feeding a browser. An export
-- prefers the text, and the difference matters: when a mail was sent as
-- multipart/alternative the text part was written by the sender's own client,
-- which lays out a quoted reply chain and a price table far better than any
-- tag-stripper of ours will. HTMLToText is the fallback, not the first choice.
SELECT 'OUT' AS direction, m.id, m.subject,
       m.body AS body_html, ''::text AS body_text, m.body_format,
       m.to_email AS counterparty, m.sender_name AS who,
       coalesce(m.sent_at, m.queued_at) AS at
FROM email_messages m
WHERE m.tenant_id = sqlc.arg(tenant_id)::bigint
  AND m.sender_id = sqlc.arg(owner_id)::bigint
  AND m.thread_key = sqlc.arg(thread_key)::text
UNION ALL
SELECT 'IN' AS direction, i.id, i.subject,
       i.body_html, i.body_text,
       CASE WHEN i.body_html <> '' THEN 'HTML' ELSE 'TEXT' END AS body_format,
       i.from_email AS counterparty, i.from_name AS who,
       coalesce(i.sent_at, i.received_at) AS at
FROM email_inbound i
WHERE i.tenant_id = sqlc.arg(tenant_id)::bigint
  AND i.owner_id = sqlc.arg(owner_id)::bigint
  AND i.thread_key = sqlc.arg(thread_key)::text
  AND NOT i.is_bounce
ORDER BY at;

-- name: ListThreadInboundFiles :many
-- Every file that arrived in this conversation, keyed to the message it came
-- with. Names and sizes only: the export lists what was attached, it does not
-- carry the bytes. A transcript that silently omitted "并附上签署版合同.pdf"
-- would read as though no contract was ever sent.
SELECT a.inbound_id AS message_id, a.file_name, a.file_size
FROM email_inbound_attachments a
JOIN email_inbound i
  ON i.id = a.inbound_id AND i.tenant_id = a.tenant_id
WHERE i.tenant_id = sqlc.arg(tenant_id)::bigint
  AND i.owner_id = sqlc.arg(owner_id)::bigint
  AND i.thread_key = sqlc.arg(thread_key)::text
ORDER BY a.inbound_id, a.id;

-- name: ListThreadSentFiles :many
-- The same for our side. Outbound attachments hang off the campaign rather
-- than the message — one upload, many recipients — so this joins back through
-- it to say which of our sends carried which file.
SELECT m.id AS message_id, a.file_name, a.file_size
FROM email_messages m
JOIN email_attachments a
  ON a.campaign_id = m.campaign_id AND a.tenant_id = m.tenant_id
WHERE m.tenant_id = sqlc.arg(tenant_id)::bigint
  AND m.sender_id = sqlc.arg(owner_id)::bigint
  AND m.thread_key = sqlc.arg(thread_key)::text
ORDER BY m.id, a.id;

-- name: RecordExport :one
-- Written before the document is handed over, never after. An export whose
-- record could not be written must not happen at all — see ExportMailThread.
INSERT INTO mail_export_log (
    tenant_id, employee_id, employee_name, thread_key, subject,
    counterparty, turn_count, byte_size, format, client_ip
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(employee_id)::bigint,
    sqlc.arg(employee_name)::text,
    sqlc.arg(thread_key)::text,
    sqlc.arg(subject)::text,
    sqlc.arg(counterparty)::text,
    sqlc.arg(turn_count)::int,
    sqlc.arg(byte_size)::bigint,
    sqlc.arg(format)::text,
    sqlc.arg(client_ip)::text
)
RETURNING id, exported_at;

-- name: ListExports :many
-- Newest first: the question this log answers is almost always about the
-- recent past. Scoped through the same visibility the team mail view uses, so
-- a sales manager sees their own team and nobody else's.
SELECT id, employee_id, employee_name, thread_key, subject, counterparty,
       turn_count, byte_size, format, client_ip, exported_at
FROM mail_export_log
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND (sqlc.arg(visible_all)::bool
       OR employee_id = ANY(sqlc.arg(visible_ids)::bigint[]))
ORDER BY exported_at DESC, id DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: CountExports :one
SELECT count(*)::bigint FROM mail_export_log
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND (sqlc.arg(visible_all)::bool
       OR employee_id = ANY(sqlc.arg(visible_ids)::bigint[]));

-- name: ListSignatures :many
-- Both layers at once: the company template and this person's own. The
-- caller decides which to offer; the query does not hide either.
SELECT id, owner_type, owner_id, name, content, body_format, is_default
FROM email_signatures
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND (owner_type = 'TENANT' OR owner_id = sqlc.arg(employee_id)::bigint)
ORDER BY owner_type, is_default DESC, id;

-- name: GetSignature :one
SELECT id, owner_type, owner_id, name, content, body_format, is_default
FROM email_signatures
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: CreateSignature :one
INSERT INTO email_signatures (tenant_id, owner_type, owner_id, name, content, body_format, is_default)
VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(owner_type)::text,
    sqlc.arg(owner_id)::bigint,
    sqlc.arg(name)::text,
    sqlc.arg(content)::text,
    sqlc.arg(body_format)::text,
    sqlc.arg(is_default)::bool
)
RETURNING id;

-- name: ClearDefaultSignature :exec
-- Run before setting a new default: the partial unique index would otherwise
-- refuse the second one, and a constraint violation is a worse message than
-- simply moving the flag.
UPDATE email_signatures SET is_default = false
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_type = sqlc.arg(owner_type)::text
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND is_default;

-- name: DeleteSignature :execrows
DELETE FROM email_signatures
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: CreateCampaign :one
INSERT INTO email_campaigns (
    tenant_id, campaign_no, subject_tpl, body_tpl, body_text_tpl, body_format,
    signature_id, kind, sender_id, sender_name, sender_email,
    scheduled_at, reply_to_inbound_id
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(campaign_no)::text,
    sqlc.arg(subject_tpl)::text,
    sqlc.arg(body_tpl)::text,
    sqlc.arg(body_text_tpl)::text,
    sqlc.arg(body_format)::text,
    nullif(sqlc.arg(signature_id)::bigint, 0),
    sqlc.arg(kind)::text,
    sqlc.arg(sender_id)::bigint,
    sqlc.arg(sender_name)::text,
    sqlc.arg(sender_email)::text,
    sqlc.narg(scheduled_at)::timestamptz,
    sqlc.arg(reply_to_inbound_id)::bigint
)
RETURNING id;

-- name: GetCampaign :one
SELECT
    c.id, c.campaign_no, c.subject_tpl, c.body_tpl, c.body_text_tpl, c.body_format,
    coalesce(c.signature_id, 0)::bigint AS signature_id,
    c.kind, c.sender_id, c.sender_name, c.sender_email, c.status,
    c.created_at, c.finished_at
FROM email_campaigns c
WHERE c.tenant_id = sqlc.arg(tenant_id)::bigint AND c.id = sqlc.arg(id)::bigint;

-- name: ListCampaigns :many
-- Counts come from the messages, not from columns on the campaign: a
-- denormalised total is one crash away from disagreeing with the rows.
SELECT
    c.id, c.campaign_no, c.subject_tpl, c.kind, c.sender_id, c.sender_name,
    c.status, c.created_at, c.finished_at,
    coalesce(m.total, 0)::int     AS total_count,
    coalesce(m.sent, 0)::int      AS sent_count,
    coalesce(m.delivered, 0)::int AS delivered_count,
    coalesce(m.failed, 0)::int    AS failed_count,
    coalesce(m.pending, 0)::int   AS pending_count,
    -- Who it went to, as a sent-mail list shows it: the first few names and
    -- a count. One bulk send is one line here even though it is N messages
    -- underneath, which is the whole point of the Sent view.
    coalesce(m.to_names, '')::text AS to_names,
    count(*) OVER () AS total
FROM email_campaigns c
LEFT JOIN (
    -- The three buckets are exhaustive by construction: "failed" is defined
    -- as whatever is neither out nor still going, so sent + pending + failed
    -- always equals total. Listing the failure statuses positively is what
    -- went wrong before — SEND_UNKNOWN belonged to no bucket, so the status
    -- that most needs a person was the one missing from the summary, and the
    -- counts silently disagreed with the total beside them.
    SELECT campaign_id,
        count(*)                                                  AS total,
        count(*) FILTER (WHERE status IN ('ACCEPTED','DELIVERED')) AS sent,
        count(*) FILTER (WHERE status = 'DELIVERED')              AS delivered,
        count(*) FILTER (WHERE status IN ('QUEUED','SENDING'))    AS pending,
        count(*) FILTER (
            WHERE status NOT IN ('ACCEPTED','DELIVERED','QUEUED','SENDING')
        )                                                          AS failed,
        -- Ordered by id so the preview is stable between refreshes rather
        -- than reshuffling on every read.
        string_agg(coalesce(nullif(to_name, ''), to_email), ', '
                   ORDER BY id) FILTER (WHERE true)                AS to_names
    FROM email_messages
    WHERE tenant_id = sqlc.arg(tenant_id)::bigint
    GROUP BY campaign_id
) m ON m.campaign_id = c.id
WHERE c.tenant_id = sqlc.arg(tenant_id)::bigint
  -- Data scope: whose sends this person may read. An empty id list with
  -- visible_all false means "nothing", which is the right answer for
  -- somebody with no scope rather than a silent widening to everything.
  AND (sqlc.arg(visible_all)::bool OR c.sender_id = ANY(sqlc.arg(visible_ids)::bigint[]))
  AND (sqlc.arg(sender_id)::bigint = 0 OR c.sender_id = sqlc.arg(sender_id)::bigint)
  AND (sqlc.arg(keyword)::text = ''
       OR c.campaign_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR c.subject_tpl ILIKE '%' || sqlc.arg(keyword)::text || '%')
ORDER BY c.id DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: FinishCampaign :exec
UPDATE email_campaigns SET status = 'DONE', finished_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint
  AND status = 'SENDING'
  AND NOT EXISTS (
      SELECT 1 FROM email_messages
      WHERE campaign_id = sqlc.arg(id)::bigint AND status IN ('QUEUED','SENDING')
  );

-- name: QueueMessage :one
-- thread_key falls back to the message's own key: a fresh mail is the root
-- of whatever conversation follows, a reply passes the thread it belongs to.
INSERT INTO email_messages (
    tenant_id, campaign_id, message_key, kind, sender_id, sender_name,
    to_email, to_name, customer_id, customer_name, contact_id,
    subject, body, body_text, body_format, status, attention_reason,
    send_mode, thread_key, in_reply_to, references_ids, scheduled_at
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    nullif(sqlc.arg(campaign_id)::bigint, 0),
    sqlc.arg(message_key)::text::uuid,
    sqlc.arg(kind)::text,
    sqlc.arg(sender_id)::bigint,
    sqlc.arg(sender_name)::text,
    sqlc.arg(to_email)::text,
    sqlc.arg(to_name)::text,
    sqlc.arg(customer_id)::bigint,
    sqlc.arg(customer_name)::text,
    sqlc.arg(contact_id)::bigint,
    sqlc.arg(subject)::text,
    sqlc.arg(body)::text,
    sqlc.arg(body_text)::text,
    sqlc.arg(body_format)::text,
    sqlc.arg(status)::text,
    sqlc.arg(attention_reason)::text,
    sqlc.arg(send_mode)::text,
    coalesce(nullif(sqlc.arg(thread_key)::text, ''), sqlc.arg(message_key)::text),
    sqlc.arg(in_reply_to)::text,
    sqlc.arg(references_ids)::text,
    sqlc.narg(scheduled_at)::timestamptz
)
RETURNING id;

-- name: AddMessageRecipient :exec
-- One person on a merged mail. Inserted in the same transaction as the
-- message, so the worker can never claim a merged send with half its list.
INSERT INTO email_message_recipients (tenant_id, message_id, kind, email, name, customer_id)
VALUES (
    sqlc.arg(tenant_id)::bigint, sqlc.arg(message_id)::bigint,
    sqlc.arg(kind)::text, sqlc.arg(email)::text, sqlc.arg(name)::text,
    sqlc.arg(customer_id)::bigint
);

-- name: ListMessageRecipients :many
SELECT id, kind, email, name, status, detail
FROM email_message_recipients
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND message_id = sqlc.arg(message_id)::bigint
ORDER BY id;

-- name: MarkRecipientResult :exec
UPDATE email_message_recipients
SET status = sqlc.arg(status)::text, detail = sqlc.arg(detail)::text
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: ClaimMessages :many
-- The worker's claim. SKIP LOCKED lets several workers drain the same queue
-- without blocking on each other, and the UPDATE to SENDING commits before
-- the provider is ever called — a row that exists in SENDING is the only
-- evidence that an attempt was made, and without it a crash mid-call leaves
-- no trace at all.
WITH due AS (
    SELECT id FROM email_messages
    WHERE tenant_id = sqlc.arg(tenant_id)::bigint
      AND status = 'QUEUED'
      AND (next_retry_at IS NULL OR next_retry_at <= now())
      -- Held back until the person asked for it to go. Separate from the
      -- retry clock above: one is "not yet", the other is "not again yet".
      AND (scheduled_at IS NULL OR scheduled_at <= now())
    ORDER BY id
    LIMIT sqlc.arg(row_limit)::int
    FOR UPDATE SKIP LOCKED
)
UPDATE email_messages m
SET status = 'SENDING', attempt_count = m.attempt_count + 1
FROM due
WHERE m.id = due.id
RETURNING m.id, m.message_key::text AS message_key, m.kind, m.to_email, m.to_name,
          m.sender_id, m.sender_name, m.subject, m.body, m.body_text, m.body_format,
          coalesce(m.campaign_id, 0)::bigint AS campaign_id, m.attempt_count,
          m.send_mode, m.in_reply_to, m.references_ids;

-- name: MarkAccepted :exec
-- tracked is written here rather than guessed later: whether a pixel went out
-- is decided at this moment, by this message's format and the address the
-- service had at the time, and neither can be recovered from the row
-- afterwards. See migration 00019.
--
-- from_email is here for the same reason and is the stronger case: the
-- address is knowable only now, while the adapter still holds the binding it
-- authenticated with. Rebinding the mailbox later does not make this mail
-- have left from somewhere else. See migration 00021.
UPDATE email_messages
SET status = 'ACCEPTED', provider_id = sqlc.arg(provider_id)::text,
    sent_at = now(), last_error = '',
    tracked = sqlc.arg(tracked)::boolean,
    from_email = sqlc.arg(from_email)::text
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: MarkRetryable :exec
UPDATE email_messages
SET status = 'QUEUED', last_error = sqlc.arg(last_error)::text,
    next_retry_at = now() + (sqlc.arg(backoff_seconds)::int || ' seconds')::interval
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: MarkTerminal :exec
-- FAILED, HARD_BOUNCED, SEND_UNKNOWN or NEEDS_ATTENTION — everything that
-- stops the automatic path and, where relevant, hands the message to a person.
UPDATE email_messages
SET status = sqlc.arg(status)::text,
    last_error = sqlc.arg(last_error)::text,
    attention_reason = sqlc.arg(attention_reason)::text
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: ReviveStuckSending :execrows
-- A message left in SENDING past the decision window means the process died
-- between the call and the response: we cannot tell whether it went out.
-- It becomes SEND_UNKNOWN rather than being silently retried.
UPDATE email_messages
SET status = 'SEND_UNKNOWN',
    attention_reason = '发送超时，无法确定是否已发出——请人工确认后决定重发或放弃'
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND status = 'SENDING'
  AND queued_at < now() - (sqlc.arg(window_seconds)::int || ' seconds')::interval;

-- name: GetMessage :one
-- The sender's address is the one stamped on the row at send time.
--
-- Two nearby columns look like the right answer and are not. campaigns
-- .sender_email is copied from the employee's HR record, a profile field that
-- carries no guarantee of being the mailbox anybody sends from. And joining
-- mail_accounts on employee_id — which this query used to do — answers a
-- different question than the one asked: not "where did this leave from" but
-- "where would it leave from if sent now". Those agree until somebody rebinds
-- their mailbox, and then every message in history silently changes sender.
--
-- So it reads email_messages.from_email, written by MarkAccepted from the
-- binding the adapter authenticated with. Empty for anything sent before
-- migration 00021, which the screen renders as "未记录" rather than filling in
-- from a guess.
SELECT
    m.id, coalesce(m.campaign_id, 0)::bigint AS campaign_id, m.message_key::text AS message_key, m.kind,
    m.sender_id, m.sender_name, m.from_email::text AS sender_email,
    m.to_email, m.to_name, m.customer_name, m.contact_id,
    m.subject, m.body, m.body_text, m.body_format, m.status, m.attempt_count, m.provider_id, m.last_error,
    m.attention_reason, m.queued_at, m.sent_at, m.delivered_at, m.opened_at, m.clicked_at,
    m.tracked
FROM email_messages m
WHERE m.tenant_id = sqlc.arg(tenant_id)::bigint AND m.id = sqlc.arg(id)::bigint;

-- name: ListMessages :many
SELECT
    id, coalesce(campaign_id, 0)::bigint AS campaign_id, kind,
    sender_id, sender_name, to_email, to_name, customer_name,
    subject, status, attempt_count, last_error, attention_reason,
    queued_at, sent_at, delivered_at, opened_at,
    count(*) OVER () AS total
FROM email_messages
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND (sqlc.arg(visible_all)::bool OR sender_id = ANY(sqlc.arg(visible_ids)::bigint[]))
  AND (sqlc.arg(sender_id)::bigint = 0 OR sender_id = sqlc.arg(sender_id)::bigint)
  AND (sqlc.arg(campaign_id)::bigint = 0 OR campaign_id = sqlc.arg(campaign_id)::bigint)
  AND (sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text)
  -- The single filter the failure page needs: everything still waiting on a
  -- person. FAILED is deliberately absent — that is the status abandoning
  -- sets, so including it meant dealing with an item never removed it from
  -- the queue and the badge could only ever count up. SOFT_BOUNCED is here
  -- because a mailbox that stayed full through every retry needs somebody to
  -- chase the contact, not another attempt.
  AND (NOT sqlc.arg(attention_only)::bool
       OR status IN ('NEEDS_ATTENTION','SEND_UNKNOWN','HARD_BOUNCED','SOFT_BOUNCED','COMPLAINED'))
  AND (sqlc.arg(keyword)::text = ''
       OR to_email ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR to_name  ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR subject  ILIKE '%' || sqlc.arg(keyword)::text || '%')
  -- Keyset. The order is by id alone, so the cursor is one: the id of the
  -- last row shown. 0 is the first page. Offset used to do this, and on a
  -- list that grows at the top it meant a message arriving mid-read could
  -- push a row across the boundary and show it twice, or hide it.
  AND (sqlc.arg(cursor_id)::bigint = 0 OR id < sqlc.arg(cursor_id)::bigint)
ORDER BY id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: RequeueMessage :execrows
-- Putting a failed message back in the queue by hand, optionally at a
-- corrected address. Attempts reset: this is a fresh decision by a person,
-- not a continuation of the automatic retries that already gave up.
UPDATE email_messages
SET status = 'QUEUED', attempt_count = 0, next_retry_at = NULL,
    last_error = '', attention_reason = '',
    to_email = CASE WHEN sqlc.arg(to_email)::text = '' THEN to_email ELSE sqlc.arg(to_email)::text END
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint
  AND status IN ('NEEDS_ATTENTION','SEND_UNKNOWN','HARD_BOUNCED','FAILED','SOFT_BOUNCED');

-- name: AbandonMessage :execrows
UPDATE email_messages
SET status = 'FAILED', attention_reason = sqlc.arg(reason)::text
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint
  AND status IN ('NEEDS_ATTENTION','SEND_UNKNOWN','HARD_BOUNCED','SOFT_BOUNCED');

-- name: AppendEvent :exec
INSERT INTO email_events (tenant_id, message_id, kind, ip, user_agent, is_proxy, target_url, detail)
VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(message_id)::bigint,
    sqlc.arg(kind)::text,
    nullif(sqlc.arg(ip)::text, '')::inet,
    sqlc.arg(user_agent)::text,
    sqlc.arg(is_proxy)::bool,
    sqlc.arg(target_url)::text,
    sqlc.arg(detail)::text
);

-- name: ListEventsOfMessage :many
SELECT id, kind, occurred_at, coalesce(host(ip), '')::text AS ip,
       user_agent, is_proxy, target_url, detail
FROM email_events
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND message_id = sqlc.arg(message_id)::bigint
ORDER BY occurred_at, id;

-- name: SuppressedAmong :many
-- Checked before queueing, in one round trip for the whole recipient list.
SELECT email, reason FROM email_suppressions
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND email = ANY(sqlc.arg(emails)::text[]);

-- name: AddSuppression :exec
INSERT INTO email_suppressions (tenant_id, email, reason, detail)
VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(email)::text,
    sqlc.arg(reason)::text,
    sqlc.arg(detail)::text
)
ON CONFLICT (tenant_id, email) DO UPDATE
SET reason = EXCLUDED.reason, detail = EXCLUDED.detail;

-- name: ListSuppressions :many
SELECT id, email, reason, detail, created_at
FROM email_suppressions
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND (sqlc.arg(keyword)::text = '' OR email ILIKE '%' || sqlc.arg(keyword)::text || '%')
ORDER BY id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: RemoveSuppression :execrows
DELETE FROM email_suppressions
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND email = sqlc.arg(email)::text;


-- ------------------------------------------------------------- attachments

-- name: AddAttachment :one
INSERT INTO email_attachments (
    tenant_id, campaign_id, file_name, file_key, file_size, content_type, uploaded_by
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(campaign_id)::bigint,
    sqlc.arg(file_name)::text,
    sqlc.arg(file_key)::text,
    sqlc.arg(file_size)::bigint,
    sqlc.arg(content_type)::text,
    sqlc.arg(uploaded_by)::bigint
)
RETURNING id;

-- name: ListAttachments :many
SELECT id, file_name, file_key, file_size, content_type, uploaded_at
FROM email_attachments
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND campaign_id = sqlc.arg(campaign_id)::bigint
ORDER BY id;

-- name: SumAttachmentSize :one
-- Guards the per-send total. Recipient servers commonly refuse anything much
-- over 25 MB, and the refusal arrives as a bounce per recipient rather than
-- as one error at compose time.
SELECT coalesce(sum(file_size), 0)::bigint AS total_bytes
FROM email_attachments
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND campaign_id = sqlc.arg(campaign_id)::bigint;

-- ------------------------------------------------------------------ images

-- name: AddImage :one
INSERT INTO email_images (
    tenant_id, token, file_name, file_key, file_size, content_type, uploaded_by
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(token)::text,
    sqlc.arg(file_name)::text,
    sqlc.arg(file_key)::text,
    sqlc.arg(file_size)::bigint,
    sqlc.arg(content_type)::text,
    sqlc.arg(uploaded_by)::bigint
)
RETURNING id;

-- name: ResolveImage :one
-- Serves the public route. Deliberately not scoped by tenant: the token is
-- the only credential, because the recipient's mail client has no session.
-- That is why the token is random and why withdrawing one has to work.
SELECT file_key, content_type
FROM email_images
WHERE token = sqlc.arg(token)::text AND status = 'ACTIVE';

-- name: ListImages :many
SELECT id, token, file_name, file_size, content_type, uploaded_at
FROM email_images
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND status = 'ACTIVE'
ORDER BY id DESC
LIMIT 200;

-- name: WithdrawImage :execrows
UPDATE email_images SET status = 'WITHDRAWN'
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;


-- name: ListSendersWithCounts :many
-- Who has been sending, for the supervisor's employee picker. Only people
-- with correspondence appear: an empty mailbox is not worth a row, and the
-- org chart is somebody else's list.
SELECT
    sender_id,
    max(sender_name)::text AS sender_name,
    count(*)::int          AS message_count,
    count(*) FILTER (
        WHERE status NOT IN ('ACCEPTED','DELIVERED','QUEUED','SENDING')
    )::int                 AS failed_count,
    max(queued_at)::timestamptz AS last_sent_at
FROM email_messages
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND (sqlc.arg(visible_all)::bool OR sender_id = ANY(sqlc.arg(visible_ids)::bigint[]))
GROUP BY sender_id
ORDER BY max(queued_at) DESC;

-- ------------------------------------------------------------------ drafts

-- name: SaveDraft :one
-- Upsert on id: the composer autosaves, so this runs repeatedly for one
-- draft and must not accumulate rows.
INSERT INTO email_drafts (
    id, tenant_id, owner_id, subject, body, body_format,
    signature_id, kind, recipients, attachments,
    send_mode, cc, bcc, reply_to_inbound_id, forward_inbound_id
) VALUES (
    coalesce(nullif(sqlc.arg(id)::bigint, 0), nextval('email_drafts_id_seq')),
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(owner_id)::bigint,
    sqlc.arg(subject)::text,
    sqlc.arg(body)::text,
    sqlc.arg(body_format)::text,
    sqlc.arg(signature_id)::bigint,
    sqlc.arg(kind)::text,
    sqlc.arg(recipients)::jsonb,
    sqlc.arg(attachments)::jsonb,
    sqlc.arg(send_mode)::text,
    sqlc.arg(cc)::jsonb,
    sqlc.arg(bcc)::jsonb,
    sqlc.arg(reply_to_inbound_id)::bigint,
    sqlc.arg(forward_inbound_id)::bigint
)
ON CONFLICT (id) DO UPDATE SET
    subject = excluded.subject,
    body = excluded.body,
    body_format = excluded.body_format,
    signature_id = excluded.signature_id,
    kind = excluded.kind,
    recipients = excluded.recipients,
    attachments = excluded.attachments,
    bcc = excluded.bcc,
    send_mode = excluded.send_mode,
    cc = excluded.cc,
    reply_to_inbound_id = excluded.reply_to_inbound_id,
    forward_inbound_id = excluded.forward_inbound_id,
    updated_at = now()
-- The owner check is in the WHERE, not just the parameters: without it an
-- upsert with somebody else's id would silently overwrite their draft.
WHERE email_drafts.tenant_id = sqlc.arg(tenant_id)::bigint
  AND email_drafts.owner_id = sqlc.arg(owner_id)::bigint
RETURNING id;

-- name: ListDrafts :many
-- Scoped to the caller, always. Drafts are not correspondence and no data
-- scope widens this.
SELECT id, subject, body_format, kind, recipients, attachments, updated_at,
       jsonb_array_length(recipients)::int AS recipient_count
FROM email_drafts
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
ORDER BY updated_at DESC
LIMIT 200;

-- name: GetDraft :one
SELECT id, subject, body, body_format, signature_id, kind,
       recipients, attachments, updated_at,
       send_mode, cc, bcc, reply_to_inbound_id, forward_inbound_id
FROM email_drafts
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND id = sqlc.arg(id)::bigint;

-- name: DeleteDraft :execrows
DELETE FROM email_drafts
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(owner_id)::bigint
  AND id = sqlc.arg(id)::bigint;

-- name: ListScheduled :many
-- The 已定时 folder: one row per send, not per recipient.
--
-- A campaign is what somebody scheduled, so it is what they cancel — and a
-- 40-recipient send appearing as 40 pending rows would make "cancel this" an
-- ambiguous instruction. Only sends with something still waiting appear:
-- cancelling every message empties the campaign out of this list without
-- needing a second status to keep in step.
SELECT
    c.id, c.campaign_no, c.subject_tpl, c.body_format, c.kind,
    m.pending::int      AS pending_count,
    m.due_at            AS scheduled_at,
    m.send_mode,
    coalesce(m.to_names, '')::text AS to_names,
    count(*) OVER () AS total
FROM email_campaigns c
JOIN (
    SELECT campaign_id,
        count(*)                          AS pending,
        min(scheduled_at)::timestamptz    AS due_at,
        min(send_mode)::text              AS send_mode,
        string_agg(coalesce(nullif(to_name, ''), to_email), ', '
                   ORDER BY id)      AS to_names
    FROM email_messages
    WHERE tenant_id = sqlc.arg(tenant_id)::bigint
      AND status = 'QUEUED'
      AND scheduled_at IS NOT NULL
      AND scheduled_at > now()
    GROUP BY campaign_id
) m ON m.campaign_id = c.id
WHERE c.tenant_id = sqlc.arg(tenant_id)::bigint
  -- Own sends only. Unlike the sent list this carries no data scope: a
  -- scheduled mail is still the sender's to change, and nobody else's.
  AND c.sender_id = sqlc.arg(sender_id)::bigint
  -- Keyset, ascending: this list is ordered by when each send is due, so it
  -- reads soonest-first and the cursor walks forward rather than back.
  AND (sqlc.narg(cursor_at)::timestamptz IS NULL
       OR (m.due_at, c.id) > (sqlc.narg(cursor_at)::timestamptz, sqlc.arg(cursor_id)::bigint))
ORDER BY m.due_at, c.id
LIMIT sqlc.arg(row_limit)::int;

-- name: CancelScheduled :execrows
-- Stops what has not gone yet, and says how much that was.
--
-- The status test is the race: the worker claims by setting SENDING inside a
-- locked transaction, so either this UPDATE gets there first and the message
-- never goes out, or it waits, sees SENDING and matches nothing. Zero rows
-- means it is already on its way — which the caller has to be told, not left
-- to assume the cancel worked.
UPDATE email_messages
SET status = 'CANCELLED', scheduled_at = NULL
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND campaign_id = sqlc.arg(campaign_id)::bigint
  AND status = 'QUEUED'
  AND scheduled_at IS NOT NULL;

-- name: ReleaseScheduled :execrows
-- "Send it now": drop the hold and let the next drain pass claim it.
UPDATE email_messages
SET scheduled_at = NULL
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND campaign_id = sqlc.arg(campaign_id)::bigint
  AND status = 'QUEUED'
  AND scheduled_at IS NOT NULL;

-- name: GetScheduledCampaign :one
-- Everything needed to prove ownership and to rebuild the compose window.
-- body_tpl, not the rendered message: the template is what a person can edit
-- again, with its variables still in it.
SELECT c.id, c.subject_tpl, c.body_tpl, c.body_format, c.kind, c.sender_id,
       c.reply_to_inbound_id, c.scheduled_at
FROM email_campaigns c
WHERE c.tenant_id = sqlc.arg(tenant_id)::bigint AND c.id = sqlc.arg(id)::bigint;

-- name: ListScheduledRecipients :many
-- Who a scheduled send was going to, from the message rows themselves. For a
-- merged send there is one message and the cast lives in the recipients
-- table, so that side is read separately.
SELECT id, to_email, to_name, customer_id, customer_name, contact_id, send_mode
FROM email_messages
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND campaign_id = sqlc.arg(campaign_id)::bigint
ORDER BY id;

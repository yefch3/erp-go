-- +goose Up

-- Where the IMAP sync left off, per mailbox.
--
-- IMAP identifies messages by a UID that is only meaningful together with the
-- mailbox's UIDVALIDITY. If the server ever changes UIDVALIDITY — a restore
-- from backup, a mailbox recreated — every UID we hold becomes meaningless
-- and the only correct response is to start again. Storing both is what makes
-- that detectable instead of silently downloading the wrong messages.
CREATE TABLE mail_sync_state (
    tenant_id     BIGINT       NOT NULL DEFAULT 1,
    account_id    BIGINT       NOT NULL,
    -- IMAP folder name. INBOX for now; Sent and others may follow.
    folder        VARCHAR(255) NOT NULL DEFAULT 'INBOX',
    uid_validity  BIGINT       NOT NULL DEFAULT 0,
    last_uid      BIGINT       NOT NULL DEFAULT 0,
    last_synced_at TIMESTAMPTZ,
    last_error    TEXT         NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, account_id, folder)
);

-- One received message.
--
-- Deliberately a separate table from email_messages rather than a `direction`
-- column on it. The two have almost nothing in common: an outbound row is a
-- delivery attempt with retries, a quota and a provider verdict; an inbound
-- row is a document somebody sent us. Sharing a table would mean every query
-- in the service growing a direction filter, and the worker's claim query
-- would have to be taught to ignore half the rows it locks.
CREATE TABLE email_inbound (
    id            BIGSERIAL    PRIMARY KEY,
    tenant_id     BIGINT       NOT NULL DEFAULT 1,
    account_id    BIGINT       NOT NULL,
    -- Whose mailbox it arrived in. Denormalised from the account so that
    -- every scope check reads the same column name as the outbound side.
    owner_id      BIGINT       NOT NULL,
    folder        VARCHAR(255) NOT NULL DEFAULT 'INBOX',
    imap_uid      BIGINT       NOT NULL,

    -- RFC 5322 Message-ID as the sender wrote it, angle brackets stripped.
    message_id    TEXT         NOT NULL DEFAULT '',
    in_reply_to   TEXT         NOT NULL DEFAULT '',
    -- Full References chain, space separated, ids without angle brackets.
    references_ids TEXT        NOT NULL DEFAULT '',

    -- The conversation this belongs to. For a reply to something we sent it
    -- is our own message_key, which is what lets a customer's answer appear
    -- underneath the mail that prompted it. Otherwise it is the root of the
    -- sender's own chain, so a thread we did not start still holds together.
    thread_key    VARCHAR(64)  NOT NULL DEFAULT '',
    -- Set when this is demonstrably a reply to one of our sends.
    reply_to_id   BIGINT,

    from_email    VARCHAR(320) NOT NULL DEFAULT '',
    from_name     VARCHAR(200) NOT NULL DEFAULT '',
    to_email      VARCHAR(320) NOT NULL DEFAULT '',
    subject       TEXT         NOT NULL DEFAULT '',
    body_html     TEXT         NOT NULL DEFAULT '',
    body_text     TEXT         NOT NULL DEFAULT '',
    -- First line or so, for the list. Computed once on ingest rather than
    -- trimmed in every query that renders a list.
    snippet       VARCHAR(300) NOT NULL DEFAULT '',

    -- The original MIME, byte for byte, in object storage.
    --
    -- Kept because forwarding has to be faithful: rebuilding a message from
    -- our parsed body loses inline images, formatting and the attachment
    -- encoding, and an export offered as evidence is worth much less if it is
    -- our reconstruction rather than what actually arrived.
    raw_key       VARCHAR(512) NOT NULL DEFAULT '',
    raw_size      BIGINT       NOT NULL DEFAULT 0,

    -- A bounce is not correspondence. It arrives in the mailbox looking like
    -- a message, and showing it as one would put "Mail Delivery Subsystem" in
    -- a salesperson's inbox instead of marking the customer unreachable.
    is_bounce     BOOLEAN      NOT NULL DEFAULT FALSE,
    is_read       BOOLEAN      NOT NULL DEFAULT FALSE,
    has_attachments BOOLEAN    NOT NULL DEFAULT FALSE,

    sent_at       TIMESTAMPTZ,
    received_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),

    -- IMAP can hand the same message twice: a retried fetch, a resumed sync.
    -- The UID within a mailbox is the server's own unique key, so this is the
    -- guard that makes ingest idempotent.
    UNIQUE (tenant_id, account_id, folder, imap_uid)
);

CREATE INDEX email_inbound_owner_idx  ON email_inbound (tenant_id, owner_id, received_at DESC);
CREATE INDEX email_inbound_thread_idx ON email_inbound (tenant_id, thread_key);
CREATE INDEX email_inbound_reply_idx  ON email_inbound (tenant_id, reply_to_id);

CREATE TABLE email_inbound_attachments (
    id           BIGSERIAL    PRIMARY KEY,
    tenant_id    BIGINT       NOT NULL DEFAULT 1,
    inbound_id   BIGINT       NOT NULL REFERENCES email_inbound(id) ON DELETE CASCADE,
    file_name    VARCHAR(255) NOT NULL,
    content_type VARCHAR(128) NOT NULL DEFAULT '',
    file_size    BIGINT       NOT NULL DEFAULT 0,
    -- Extracted to object storage on ingest. The raw MIME still holds the
    -- original bytes, so this is a convenience copy for downloading one file
    -- without re-parsing a 20 MB message.
    file_key     VARCHAR(512) NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX email_inbound_att_idx ON email_inbound_attachments (tenant_id, inbound_id);

-- Outbound messages gain the thread key so a sent mail and the replies to it
-- can be pulled together without joining through the campaign.
ALTER TABLE email_messages ADD COLUMN thread_key VARCHAR(64) NOT NULL DEFAULT '';
CREATE INDEX email_messages_thread_idx ON email_messages (tenant_id, thread_key);

-- Existing rows: their own key is the root of whatever conversation follows.
UPDATE email_messages SET thread_key = message_key WHERE thread_key = '';

-- +goose Down
DROP TABLE email_inbound_attachments;
DROP TABLE email_inbound;
DROP TABLE mail_sync_state;
DROP INDEX IF EXISTS email_messages_thread_idx;
ALTER TABLE email_messages DROP COLUMN thread_key;

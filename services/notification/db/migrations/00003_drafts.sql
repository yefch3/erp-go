-- +goose Up

-- Unfinished mail.
--
-- A separate table rather than an email_campaigns row with status DRAFT, for
-- two reasons that both matter:
--
--   * A campaign draws a number the moment it is created. A draft that is
--     abandoned would burn one, leaving permanent gaps in a sequence people
--     read as a record.
--   * A campaign's recipients are rows in email_messages — real, queued,
--     claimable by the worker. A draft's recipients are a list somebody is
--     still editing. Storing them as messages would mean the worker could
--     pick up a half-written mail.
--
-- So recipients and attachments live here as JSONB: they are notes toward a
-- send, not the send itself. Promoting a draft runs the ordinary create path,
-- which is what draws the number and writes the message rows.
CREATE TABLE email_drafts (
    id           BIGSERIAL    PRIMARY KEY,
    tenant_id    BIGINT       NOT NULL DEFAULT 1,
    -- Drafts are personal. A supervisor with ALL scope reads their team's
    -- correspondence, but a draft is not correspondence — it is unfinished
    -- thinking, and nobody has said it yet. There is deliberately no query
    -- in this service that returns another person's drafts.
    owner_id     BIGINT       NOT NULL,
    subject      TEXT         NOT NULL DEFAULT '',
    body         TEXT         NOT NULL DEFAULT '',
    body_format  VARCHAR(8)   NOT NULL DEFAULT 'HTML'
        CHECK (body_format IN ('TEXT','HTML')),
    signature_id BIGINT       NOT NULL DEFAULT 0,
    kind         VARCHAR(16)  NOT NULL DEFAULT 'MARKETING'
        CHECK (kind IN ('MARKETING','TRANSACTIONAL')),
    -- [{contactId, name, email, customerId, customerName}, ...]
    recipients   JSONB        NOT NULL DEFAULT '[]'::jsonb,
    -- [{fileName, fileKey, size}, ...] — already in object storage, not yet
    -- registered against any campaign, because there is no campaign yet.
    attachments  JSONB        NOT NULL DEFAULT '[]'::jsonb,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX email_drafts_owner_idx ON email_drafts (tenant_id, owner_id, updated_at DESC);

-- +goose Down
DROP TABLE email_drafts;

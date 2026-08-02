-- +goose Up

-- Merged sends and reply threading.
--
-- A merged send is ONE mail whose recipients are meant to see each other:
-- the same bytes go out once, in a single SMTP transaction with one RCPT TO
-- per person. That is the opposite promise of the per-recipient campaign, so
-- it is a declared mode on the message rather than a bigger recipient loop.
ALTER TABLE email_messages ADD COLUMN send_mode VARCHAR(10) NOT NULL DEFAULT 'SEPARATE'
    CHECK (send_mode IN ('SEPARATE','MERGED'));

-- Our side of reply threading: the Message-ID being answered and the chain
-- above it. They become the In-Reply-To and References headers, which is
-- what makes the customer's own client stack our answer under their question.
ALTER TABLE email_messages ADD COLUMN in_reply_to TEXT NOT NULL DEFAULT '';
ALTER TABLE email_messages ADD COLUMN references_ids TEXT NOT NULL DEFAULT '';

-- Everybody on a merged mail, To and CC alike, with the per-RCPT verdict
-- from the shared transaction. SEPARATE messages have no rows here — their
-- one recipient lives on the message itself.
CREATE TABLE email_message_recipients (
    id          BIGSERIAL    PRIMARY KEY,
    tenant_id   BIGINT       NOT NULL DEFAULT 1,
    message_id  BIGINT       NOT NULL REFERENCES email_messages(id) ON DELETE CASCADE,
    kind        VARCHAR(2)   NOT NULL DEFAULT 'TO' CHECK (kind IN ('TO','CC')),
    email       VARCHAR(320) NOT NULL,
    name        VARCHAR(200) NOT NULL DEFAULT '',
    customer_id BIGINT       NOT NULL DEFAULT 0,
    -- The host's answer to this recipient's RCPT TO. PENDING until the
    -- transaction runs; a REJECTED row names exactly who did not get the
    -- mail the others received.
    status      VARCHAR(10)  NOT NULL DEFAULT 'PENDING'
        CHECK (status IN ('PENDING','ACCEPTED','REJECTED')),
    detail      TEXT         NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX email_message_recipients_idx ON email_message_recipients (tenant_id, message_id);

-- +goose Down
DROP TABLE email_message_recipients;
ALTER TABLE email_messages DROP COLUMN send_mode;
ALTER TABLE email_messages DROP COLUMN in_reply_to;
ALTER TABLE email_messages DROP COLUMN references_ids;

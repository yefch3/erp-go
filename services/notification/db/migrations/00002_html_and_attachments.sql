-- +goose Up

-- Rich text and attachments.
--
-- Two things worth stating before the columns, because they drive the shape:
--
-- 1. Email HTML is not web HTML. Outlook renders with Word's engine; Gmail
--    strips <style> blocks entirely. So the stored HTML must already carry
--    inline styles and must stay inside a small tag whitelist. The editor
--    produces that shape and the server enforces it — the client is never
--    the security boundary.
--
-- 2. An HTML mail must carry a plain-text alternative. Sending HTML alone is
--    a measurable spam signal, and some recipients genuinely read in text.
--    So body holds the HTML and body_text holds the alternative; the worker
--    hands both to the provider as multipart/alternative.

ALTER TABLE email_campaigns
    ADD COLUMN body_format VARCHAR(8) NOT NULL DEFAULT 'TEXT'
        CHECK (body_format IN ('TEXT','HTML')),
    -- The plain-text alternative, still holding {{variables}}.
    ADD COLUMN body_text_tpl TEXT NOT NULL DEFAULT '';

ALTER TABLE email_messages
    ADD COLUMN body_format VARCHAR(8) NOT NULL DEFAULT 'TEXT'
        CHECK (body_format IN ('TEXT','HTML')),
    -- Rendered plain-text alternative. Empty when body_format = 'TEXT',
    -- because then body is already the text.
    ADD COLUMN body_text TEXT NOT NULL DEFAULT '';

ALTER TABLE email_signatures
    ADD COLUMN body_format VARCHAR(8) NOT NULL DEFAULT 'TEXT'
        CHECK (body_format IN ('TEXT','HTML'));

-- Files sent with a mail. Campaign-scoped, not message-scoped: every
-- recipient of one send gets the same files, and a row per recipient would
-- store the same key 500 times.
--
-- Note this is where bulk sending gets expensive in a way that is invisible
-- at compose time: a 5 MB attachment to 500 recipients is 2.5 GB pushed
-- through the provider. The UI warns; the schema just records.
CREATE TABLE email_attachments (
    id           BIGSERIAL    PRIMARY KEY,
    tenant_id    BIGINT       NOT NULL DEFAULT 1,
    campaign_id  BIGINT       NOT NULL REFERENCES email_campaigns(id) ON DELETE CASCADE,
    file_name    VARCHAR(255) NOT NULL,
    -- Object-storage key, never a local path.
    file_key     VARCHAR(500) NOT NULL,
    file_size    BIGINT       NOT NULL DEFAULT 0,
    content_type VARCHAR(100) NOT NULL DEFAULT '',
    uploaded_by  BIGINT       NOT NULL DEFAULT 0,
    uploaded_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX email_attachments_campaign_idx
    ON email_attachments (tenant_id, campaign_id);

-- Images referenced from inside HTML bodies and signatures — company logo,
-- product shots.
--
-- These are NOT attachments: they are fetched over HTTP by the recipient's
-- client when the mail is opened, which may be weeks later. That rules out
-- presigned URLs, which expire and would leave a broken image in every mail
-- already sent. Instead each image gets an unguessable token and is served
-- by a public, read-only route that resolves the token to an object key.
--
-- Keeping a row per image (rather than deriving the URL from the key) is what
-- makes revocation and cleanup possible: an image can be withdrawn without
-- hunting through every signature that embedded it.
CREATE TABLE email_images (
    id           BIGSERIAL    PRIMARY KEY,
    tenant_id    BIGINT       NOT NULL DEFAULT 1,
    -- Random, unguessable; this is what appears in the public URL.
    token        VARCHAR(64)  NOT NULL,
    file_name    VARCHAR(255) NOT NULL,
    file_key     VARCHAR(500) NOT NULL,
    file_size    BIGINT       NOT NULL DEFAULT 0,
    content_type VARCHAR(100) NOT NULL DEFAULT '',
    -- Withdrawn images stop being served but the row stays, so a support
    -- question about a broken logo has an answer.
    status       VARCHAR(16)  NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE','WITHDRAWN')),
    uploaded_by  BIGINT       NOT NULL DEFAULT 0,
    uploaded_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (token)
);
CREATE INDEX email_images_tenant_idx ON email_images (tenant_id, id DESC);

-- +goose Down
DROP TABLE email_images;
DROP TABLE email_attachments;
ALTER TABLE email_signatures DROP COLUMN body_format;
ALTER TABLE email_messages   DROP COLUMN body_text, DROP COLUMN body_format;
ALTER TABLE email_campaigns  DROP COLUMN body_text_tpl, DROP COLUMN body_format;

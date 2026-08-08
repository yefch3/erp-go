-- +goose Up

-- Pictures in received mail, fetched by us and kept as our own copy.
--
-- Until now a received mail's pictures were still hosted by whoever sent it,
-- and the browser fetched them at the moment somebody opened the mail. Three
-- things followed from that, and all three are why this table exists:
--
--   1. Opening a mail told the sender it had been opened — including the
--      one-pixel invisible images that exist for no other purpose. Our own
--      product refuses to send those; firing other people's on our reader's
--      behalf was the same act from the other side.
--   2. It told them the reader's IP address and the exact minute, every time.
--   3. A conversation view builds every message in the thread, even the
--      folded ones, so opening one exchange fired the pixels of fifteen mails
--      nobody had looked at. Measured on a real thread: 378 pictures
--      referenced from folded messages, 99 of them actually downloaded.
--
-- Fetching at delivery instead of at reading breaks the link between "a
-- picture was requested" and "a person read this". It is what Gmail has done
-- since 2013 and what Apple Mail has done since 2021, and it is the only
-- approach that keeps the pictures visible — blocking them outright, the
-- Thunderbird/Outlook default, means a customer's product photo needs a click
-- every time, which in this business is every mail.
CREATE TABLE email_inbound_images (
    id           BIGSERIAL    PRIMARY KEY,
    tenant_id    BIGINT       NOT NULL DEFAULT 1,
    inbound_id   BIGINT       NOT NULL REFERENCES email_inbound(id) ON DELETE CASCADE,

    -- The address the mail pointed at. Kept in full because the read path
    -- matches on it: the stored body is left exactly as it arrived, and the
    -- substitution happens on the way out.
    --
    -- Storing the original body rather than a rewritten one is deliberate. It
    -- keeps what we hold faithful to what was sent, it means a lost cache
    -- degrades to "the picture loads from the sender" instead of "the picture
    -- is gone", and it makes this table re-buildable at any time.
    source_url   TEXT         NOT NULL,
    -- sha256 of source_url. The uniqueness constraint cannot sit on the URL
    -- itself: btree entries are capped around 2700 bytes and tracking URLs
    -- run past that surprisingly often.
    url_hash     BYTEA        NOT NULL,

    object_key   VARCHAR(512) NOT NULL,
    -- Sniffed from the bytes, never taken from the sender's Content-Type.
    -- This value is handed back to the browser when the picture is served,
    -- so trusting the header would let a sender have text/html rendered from
    -- the storage origin.
    content_type VARCHAR(64)  NOT NULL DEFAULT '',
    byte_size    BIGINT       NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),

    UNIQUE (tenant_id, inbound_id, url_hash)
);

CREATE INDEX email_inbound_images_mail_idx
    ON email_inbound_images (tenant_id, inbound_id);

-- When the caching pass last looked at this message. NULL means "not yet",
-- which is both the queue for new mail and, on the day this ships, the
-- backfill for everything already in the mailbox.
ALTER TABLE email_inbound ADD COLUMN images_cached_at TIMESTAMPTZ;

-- The work queue. Partial, so it shrinks to nothing once the backlog clears
-- rather than carrying a row per message for ever.
CREATE INDEX email_inbound_images_pending_idx
    ON email_inbound (tenant_id, id)
    WHERE images_cached_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS email_inbound_images_pending_idx;
ALTER TABLE email_inbound DROP COLUMN IF EXISTS images_cached_at;
DROP TABLE IF EXISTS email_inbound_images;

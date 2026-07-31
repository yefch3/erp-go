-- +goose Up

-- Signature blocks. Two layers on purpose: a company-wide template keeps the
-- outgoing format consistent, and an individual copy carries the extension
-- number. Variables inside a signature mean the company template can be
-- changed once instead of chased across every employee.
CREATE TABLE email_signatures (
    id           BIGSERIAL    PRIMARY KEY,
    tenant_id    BIGINT       NOT NULL DEFAULT 1,
    owner_type   VARCHAR(16)  NOT NULL DEFAULT 'EMPLOYEE'
        CHECK (owner_type IN ('TENANT','EMPLOYEE')),
    owner_id     BIGINT       NOT NULL DEFAULT 0,
    name         VARCHAR(100) NOT NULL,
    content      TEXT         NOT NULL,
    is_default   BOOLEAN      NOT NULL DEFAULT false,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);
-- One default per owner, enforced rather than hoped for.
CREATE UNIQUE INDEX email_signatures_default_idx
    ON email_signatures (tenant_id, owner_type, owner_id) WHERE is_default;

-- A bulk send. The body here still holds {{variables}}; what each recipient
-- actually received is stored on their own row, because that is the thing
-- somebody will need to produce a year later.
CREATE TABLE email_campaigns (
    id           BIGSERIAL    PRIMARY KEY,
    tenant_id    BIGINT       NOT NULL DEFAULT 1,
    campaign_no  VARCHAR(32)  NOT NULL,
    subject_tpl  TEXT         NOT NULL,
    body_tpl     TEXT         NOT NULL,
    signature_id BIGINT,
    -- MARKETING or TRANSACTIONAL. This is not a label: it decides what
    -- happens when a send times out and we cannot tell whether it went
    -- (see §5.12.3.1). Marketing would rather miss than duplicate; a
    -- quotation would rather duplicate than miss.
    kind         VARCHAR(16)  NOT NULL DEFAULT 'MARKETING'
        CHECK (kind IN ('MARKETING','TRANSACTIONAL')),
    sender_id    BIGINT       NOT NULL,
    sender_name  VARCHAR(100) NOT NULL DEFAULT '',
    sender_email VARCHAR(320) NOT NULL DEFAULT '',
    status       VARCHAR(16)  NOT NULL DEFAULT 'SENDING'
        CHECK (status IN ('DRAFT','SENDING','DONE','CANCELLED')),
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    finished_at  TIMESTAMPTZ,
    UNIQUE (tenant_id, campaign_no)
);

-- One row per recipient. Never one row per send with a list of addresses:
-- a shared row could not carry per-person status, could not be retried
-- individually, and could not stop the recipients seeing each other.
CREATE TABLE email_messages (
    id            BIGSERIAL     PRIMARY KEY,
    tenant_id     BIGINT        NOT NULL DEFAULT 1,
    campaign_id   BIGINT        REFERENCES email_campaigns(id) ON DELETE CASCADE,
    -- Generated before the provider is called and carried as a tag on the
    -- request, so a timed-out send can be looked up afterwards. Mail
    -- providers do not offer request-level idempotency the way payment
    -- processors do, so this identifies rather than deduplicates.
    message_key   UUID          NOT NULL,
    kind          VARCHAR(16)   NOT NULL DEFAULT 'MARKETING',
    sender_id     BIGINT        NOT NULL DEFAULT 0,
    sender_name   VARCHAR(100)  NOT NULL DEFAULT '',
    to_email      VARCHAR(320)  NOT NULL,
    to_name       VARCHAR(200)  NOT NULL DEFAULT '',
    -- Snapshots so a year-old send still reads correctly after the contact
    -- has been renamed or moved.
    customer_id   BIGINT        NOT NULL DEFAULT 0,
    customer_name VARCHAR(200)  NOT NULL DEFAULT '',
    contact_id    BIGINT        NOT NULL DEFAULT 0,
    -- Rendered, not templated: this is the exact text that was sent.
    subject       TEXT          NOT NULL,
    body          TEXT          NOT NULL,
    status        VARCHAR(20)   NOT NULL DEFAULT 'QUEUED'
        CHECK (status IN ('QUEUED','SENDING','ACCEPTED','DELIVERED','SEND_UNKNOWN',
                          'SOFT_BOUNCED','HARD_BOUNCED','COMPLAINED','FAILED','NEEDS_ATTENTION')),
    attempt_count INT           NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ,
    provider_id   VARCHAR(128)  NOT NULL DEFAULT '',
    last_error    TEXT          NOT NULL DEFAULT '',
    -- Why a human has to look at it, in words they can act on.
    attention_reason TEXT       NOT NULL DEFAULT '',
    queued_at     TIMESTAMPTZ   NOT NULL DEFAULT now(),
    sent_at       TIMESTAMPTZ,
    delivered_at  TIMESTAMPTZ,
    opened_at     TIMESTAMPTZ,
    clicked_at    TIMESTAMPTZ,
    UNIQUE (tenant_id, message_key)
);
-- The worker's claim query: queued and due, oldest first.
CREATE INDEX email_messages_due_idx
    ON email_messages (tenant_id, next_retry_at, id) WHERE status = 'QUEUED';
CREATE INDEX email_messages_campaign_idx ON email_messages (tenant_id, campaign_id, id);
CREATE INDEX email_messages_sender_idx   ON email_messages (tenant_id, sender_id, id DESC);
-- The human queue. Partial, because it is the only slice anybody opens.
CREATE INDEX email_messages_attention_idx
    ON email_messages (tenant_id, id DESC)
    WHERE status IN ('NEEDS_ATTENTION','SEND_UNKNOWN','HARD_BOUNCED');

-- Delivery feedback, append-only. Opens and clicks are recorded but treated
-- as weak signals: Apple's privacy proxy pre-fetches images so a message can
-- look opened before anybody read it, and clients that block images make a
-- genuine read invisible. is_proxy marks what we could identify.
CREATE TABLE email_events (
    id          BIGSERIAL    PRIMARY KEY,
    tenant_id   BIGINT       NOT NULL DEFAULT 1,
    message_id  BIGINT       NOT NULL REFERENCES email_messages(id) ON DELETE CASCADE,
    kind        VARCHAR(16)  NOT NULL
        CHECK (kind IN ('SENT','DELIVERED','OPEN','CLICK','BOUNCE','COMPLAINT','FAILED')),
    occurred_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    ip          INET,
    user_agent  TEXT         NOT NULL DEFAULT '',
    is_proxy    BOOLEAN      NOT NULL DEFAULT false,
    target_url  TEXT         NOT NULL DEFAULT '',
    detail      TEXT         NOT NULL DEFAULT ''
);
CREATE INDEX email_events_msg_idx ON email_events (tenant_id, message_id, occurred_at);

-- Addresses nothing may be sent to again: hard bounces and unsubscribes.
-- Tenant-wide, checked before a message is ever queued. Sending to a dead
-- address repeatedly is the fastest way to ruin a sending domain, and an
-- unsubscribe that one campaign honours and the next ignores is worse than
-- not offering one.
CREATE TABLE email_suppressions (
    id         BIGSERIAL    PRIMARY KEY,
    tenant_id  BIGINT       NOT NULL DEFAULT 1,
    email      VARCHAR(320) NOT NULL,
    reason     VARCHAR(24)  NOT NULL
        CHECK (reason IN ('HARD_BOUNCE','COMPLAINT','UNSUBSCRIBE','MANUAL')),
    detail     TEXT         NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, email)
);

-- +goose Down
DROP TABLE email_suppressions;
DROP TABLE email_events;
DROP TABLE email_messages;
DROP TABLE email_campaigns;
DROP TABLE email_signatures;

-- +goose Up

-- How this tenant reaches its mail host.
--
-- 263 is a mail *host*, not an API service: SMTP out, IMAP in, no webhooks.
-- The hostnames, ports and TLS mode come off the 263 admin console and differ
-- between their packages, so they are configuration rather than constants —
-- getting a port wrong should be a form field somebody fixes, not a release.
--
-- One row per tenant. Nothing secret lives here: the credentials are personal
-- and live in mail_accounts below.
CREATE TABLE mail_hosts (
    tenant_id      BIGINT       PRIMARY KEY DEFAULT 1,
    -- The domain mail is sent from, e.g. sunrise.com. Used to check that an
    -- employee's address actually belongs to this tenant before we try to
    -- authenticate as them somewhere.
    domain         VARCHAR(255) NOT NULL DEFAULT '',
    smtp_host      VARCHAR(255) NOT NULL DEFAULT '',
    smtp_port      INT          NOT NULL DEFAULT 465,
    -- SSL means TLS from the first byte (usually 465); STARTTLS upgrades a
    -- plaintext connection (usually 587). NONE exists only for a local test
    -- server such as MailHog and must never be used against 263.
    smtp_security  VARCHAR(8)   NOT NULL DEFAULT 'SSL'
        CHECK (smtp_security IN ('SSL','STARTTLS','NONE')),
    imap_host      VARCHAR(255) NOT NULL DEFAULT '',
    imap_port      INT          NOT NULL DEFAULT 993,
    imap_security  VARCHAR(8)   NOT NULL DEFAULT 'SSL'
        CHECK (imap_security IN ('SSL','STARTTLS','NONE')),
    -- 263 throttles per mailbox per hour and will start deferring, then
    -- blocking, a mailbox that pushes past its package limit. The worker
    -- paces itself against this rather than discovering the limit by being
    -- rate-limited mid-campaign. Deliberately conservative by default.
    hourly_quota   INT          NOT NULL DEFAULT 100
        CHECK (hourly_quota > 0),
    daily_quota    INT          NOT NULL DEFAULT 500
        CHECK (daily_quota > 0),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- One mailbox, belonging to one employee.
--
-- Everybody sends as themselves. A customer replying to lina@sunrise.com
-- reaches Lina, which is how the trade actually works — the buyer has a
-- person, not a company alias — and it means an employee's inbox is theirs
-- by construction rather than by a filter we have to get right.
--
-- The price is that this table holds credentials, so:
--
--   * secret_enc is AES-256-GCM ciphertext under a key from the environment.
--     The plaintext is never written to a column, never logged, and no read
--     path in this service returns it. The only consumer is the SMTP/IMAP
--     dialler, which decrypts it, uses it and drops it.
--   * The employee types their own authorisation code. Nobody — not an
--     administrator, not this codebase — asks anyone else for it, and there
--     is no endpoint that sets somebody else's.
--
-- 263 issues a separate client authorisation code for most enterprise
-- packages, so this is usually not the account's login password. That is
-- worth preserving: a leak here does not hand over the web mailbox.
CREATE TABLE mail_accounts (
    id           BIGSERIAL    PRIMARY KEY,
    tenant_id    BIGINT       NOT NULL DEFAULT 1,
    employee_id  BIGINT       NOT NULL,
    email        VARCHAR(320) NOT NULL,
    -- The SMTP/IMAP username. Usually the full address, but some hosts want
    -- the local part alone, so it is stored rather than derived.
    username     VARCHAR(320) NOT NULL DEFAULT '',
    secret_enc   BYTEA        NOT NULL,
    -- Which key encrypted secret_enc. Lets a key be rotated without a flag
    -- day: new rows use the new key, old rows stay readable until re-entered.
    key_version  INT          NOT NULL DEFAULT 1,
    -- Set by the connection test, so a mailbox that was never proven to work
    -- can be told apart from one that has simply not sent anything yet.
    verified_at  TIMESTAMPTZ,
    last_error   TEXT         NOT NULL DEFAULT '',
    -- Paused accounts are skipped by both the sender and the IMAP sync. An
    -- employee who has left keeps their history without us holding a live
    -- connection to their mailbox.
    is_active    BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    -- One mailbox per person: two rows would make "which account sent this"
    -- ambiguous at exactly the moment somebody is trying to trace a message.
    UNIQUE (tenant_id, employee_id),
    UNIQUE (tenant_id, email)
);

-- What the sender pacing reads. One row per account per hour, incremented as
-- messages are accepted; the worker refuses to start a message that would
-- cross the quota rather than letting 263 refuse it, because a refusal there
-- costs the mailbox reputation and this costs a delay.
CREATE TABLE mail_send_counters (
    tenant_id   BIGINT      NOT NULL DEFAULT 1,
    account_id  BIGINT      NOT NULL,
    -- Truncated to the hour.
    window_at   TIMESTAMPTZ NOT NULL,
    sent_count  INT         NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, account_id, window_at)
);

CREATE INDEX mail_accounts_active_idx
    ON mail_accounts (tenant_id, is_active) WHERE is_active;

-- +goose Down
DROP TABLE mail_send_counters;
DROP TABLE mail_accounts;
DROP TABLE mail_hosts;

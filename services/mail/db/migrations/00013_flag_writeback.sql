-- +goose Up

-- Housekeeping done in the ERP has to reach the real mailbox, or the two
-- disagree the moment somebody opens Gmail: read here, unread there. Every
-- mail client resolves this the same way — the server holds the state, the
-- client pushes its changes up and reads the result back down.
--
-- Pushing inline would make a click wait on a round trip to Gmail and lose
-- the change whenever that round trip failed. So the change lands locally
-- first (what the person sees), and the intent to publish it is queued here.
CREATE TABLE mail_flag_ops (
    id          BIGSERIAL    PRIMARY KEY,
    tenant_id   BIGINT       NOT NULL DEFAULT 1,
    account_id  BIGINT       NOT NULL,
    -- Whose mailbox: credentials are resolved per employee, and the worker
    -- needs to log in as somebody to publish anything.
    employee_id BIGINT       NOT NULL,
    -- Our logical folder name (INBOX/JUNK). The host's own name for it is
    -- resolved at write time: it differs per provider and can be renamed
    -- between queueing and sending.
    folder      VARCHAR(255) NOT NULL,
    imap_uid    BIGINT       NOT NULL,
    -- SEEN / UNSEEN for now; starring and deletion follow the same path.
    op          VARCHAR(16)  NOT NULL,
    attempts    INT          NOT NULL DEFAULT 0,
    last_error  TEXT         NOT NULL DEFAULT '',
    next_try_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),

    -- One pending intent per message: a mail toggled read/unread five times
    -- while the network is down should publish once, as whatever it ended up
    -- being. Last write wins, which is also what the person expects to see.
    UNIQUE (tenant_id, account_id, folder, imap_uid)
);

CREATE INDEX mail_flag_ops_due_idx ON mail_flag_ops (next_try_at);

-- +goose Down
DROP TABLE mail_flag_ops;

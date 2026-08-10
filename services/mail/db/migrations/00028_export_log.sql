-- +goose Up

-- Every conversation that left the building, and who carried it out.
--
-- This table is the reason the export feature is safe to have. An export is
-- the one action in the mail module that produces a file the company no
-- longer controls: it can be mailed on, copied to a phone, or taken to the
-- next employer. Nothing here prevents that. What it does is make the act
-- non-deniable — a row is written before the bytes are handed over, and if
-- the row cannot be written the export does not happen.
--
-- The subject and the counterparty are copied in rather than referenced.
-- Deliberate, and it is a real trade: an administrator reading this log can
-- see the subject lines of conversations they are not otherwise entitled to
-- read. A log that recorded only a thread key would be useless six months
-- later, when the question is "what did they take with them" and the mail
-- itself has been deleted — which is exactly when it gets asked. The reading
-- of it is gated behind its own permission for that reason.
CREATE TABLE mail_export_log (
    id            BIGSERIAL    PRIMARY KEY,
    tenant_id     BIGINT       NOT NULL DEFAULT 1,

    -- Who. The name is denormalised beside the id because an employee who
    -- leaves is exactly the employee this log is asked about, and by then
    -- their row may be gone or renamed.
    employee_id   BIGINT       NOT NULL,
    employee_name VARCHAR(200) NOT NULL DEFAULT '',

    -- What.
    thread_key    VARCHAR(64)  NOT NULL DEFAULT '',
    subject       TEXT         NOT NULL DEFAULT '',
    counterparty  VARCHAR(320) NOT NULL DEFAULT '',
    turn_count    INT          NOT NULL DEFAULT 0,
    byte_size     BIGINT       NOT NULL DEFAULT 0,
    -- The shape the document was rendered in. One value today; a column
    -- because "they exported it as X" is the sort of thing that is impossible
    -- to reconstruct afterwards and trivial to record now.
    format        VARCHAR(16)  NOT NULL DEFAULT 'HTML',

    -- From where. Only as trustworthy as the gateway's proxy configuration
    -- (see TRUST_PROXY_HEADERS) and never load-bearing on its own — but the
    -- difference between "from the office" and "from an address nobody
    -- recognises at 2am" is the first question anybody asks of a log like
    -- this, and it cannot be answered later.
    client_ip     VARCHAR(64)  NOT NULL DEFAULT '',

    exported_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- The two questions this log gets asked: "what left recently" and "what did
-- this person take". Both are answered newest-first.
CREATE INDEX mail_export_log_at_idx
    ON mail_export_log (tenant_id, exported_at DESC);
CREATE INDEX mail_export_log_who_idx
    ON mail_export_log (tenant_id, employee_id, exported_at DESC);

-- +goose Down
DROP TABLE mail_export_log;

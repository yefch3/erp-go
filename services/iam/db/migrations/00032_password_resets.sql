-- +goose Up

-- One-time links that let somebody who cannot log in choose a new password.
--
-- Same anatomy as employee_invitations and for the same reasons: sha256 of
-- the token, never the token (every live row is a key to somebody's account,
-- and a hashed table opens nothing); the address the link was mailed to kept
-- beside it (the token proves control of THAT mailbox — if the record's
-- address has drifted since, redeeming would hand the account to a mailbox
-- that never earned it); at most one live link per person (a re-request
-- replaces, it does not accumulate keys nobody is tracking).
--
-- Where it differs from an invitation, the difference is the point:
--   * One hour, not seven days. An invitation waits for somebody who did not
--     ask for it; a reset answers somebody staring at their inbox right now.
--   * requested_by 0 means the person asked from the login page themselves;
--     otherwise it is the administrator who pressed the button. Recorded so
--     abuse of the admin path has a name on it.
CREATE TABLE password_resets (
    id           BIGSERIAL    PRIMARY KEY,
    tenant_id    BIGINT       NOT NULL,
    employee_id  BIGINT       NOT NULL REFERENCES employees(id),
    email        VARCHAR(200) NOT NULL,
    token_hash   BYTEA        NOT NULL UNIQUE,
    expires_at   TIMESTAMPTZ  NOT NULL,
    used_at      TIMESTAMPTZ,
    requested_by BIGINT       NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX password_resets_live
    ON password_resets (tenant_id, employee_id)
    WHERE used_at IS NULL;

-- A password the administrator typed is a password the administrator knows.
-- For accounts that have no mailbox (username logins — the only place an
-- admin-typed password still exists), the window where that knowledge could
-- impersonate is closed by forcing a change at first login: the initial
-- password opens exactly one door, the one to the change-password form.
ALTER TABLE users ADD COLUMN must_change_password BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE users DROP COLUMN must_change_password;
DROP TABLE password_resets;

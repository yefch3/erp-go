-- +goose Up

-- The one-time links that turn an imported employee row into an account.
--
-- This table is the mechanism behind the rule the whole login design rests on:
-- only somebody the company actually gave a mailbox can log in. An
-- administrator can type any address into an employee record — that asserts
-- nothing. Sending a link to it and having somebody open it is the proof, and
-- the proof is recorded as employees.email_verified_at.
--
-- Two things follow from that, and both are enforced here rather than in Go.
CREATE TABLE employee_invitations (
    id          BIGSERIAL    PRIMARY KEY,
    tenant_id   BIGINT       NOT NULL,
    employee_id BIGINT       NOT NULL REFERENCES employees(id),

    -- The address this link was sent to, kept beside the token on purpose.
    --
    -- The token proves control of *this* mailbox, not of whatever address the
    -- employee record happens to hold when the link is opened. Without this
    -- column an administrator could invite alice@company.com, edit the row to
    -- their own address before she opens it, and activate her account from the
    -- mail she never received. Activation compares the two and refuses if they
    -- have drifted apart.
    email       VARCHAR(200) NOT NULL,

    -- sha256 of the token, never the token.
    --
    -- Every live row here is a key to an account that has no password yet, so
    -- a read of this table would be account takeover for every pending
    -- employee at once — exactly the shape of a leak that a backup copy, a
    -- support query or a stray log line produces. Hashing means the stored
    -- value opens nothing.
    --
    -- sha256 rather than argon2, unlike passwords: this is 256 bits of our own
    -- randomness with no structure to guess at, so a slow hash would buy
    -- nothing and would put an argon2 run on an unauthenticated route.
    token_hash  BYTEA        NOT NULL UNIQUE,

    expires_at  TIMESTAMPTZ  NOT NULL,
    used_at     TIMESTAMPTZ,

    -- Whose mailbox the invitation went out through. Every mail this system
    -- sends leaves from some employee's own bound mailbox, so an invitation
    -- always has a person behind it, and when one lands in a spam folder the
    -- first useful question is which mailbox it was sent from.
    invited_by  BIGINT       NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- At most one live invitation per employee.
--
-- Re-inviting is common — the first mail goes to spam, or the address was
-- wrong — and the obvious implementation of it, inserting another row, leaves
-- the previous key alive. Do that four times and an employee has four working
-- links, three of which nobody is tracking. The service deletes the old row
-- before inserting; this index is what makes that a rule rather than a habit.
CREATE UNIQUE INDEX employee_invitations_live
    ON employee_invitations (tenant_id, employee_id)
    WHERE used_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS employee_invitations;

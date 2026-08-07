-- +goose Up

-- The companies using this system, and the mail domains that identify them.
--
-- Until now tenant_id was a column with DEFAULT 1 and nothing behind it: one
-- company, hard-coded. This is the table that was missing, and it has to exist
-- before login changes, because the address somebody types at the login page
-- is what decides which company they are logging in to.
CREATE TABLE tenants (
    id         BIGSERIAL    PRIMARY KEY,
    name       VARCHAR(200) NOT NULL,
    status     VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE'
               CHECK (status IN ('ACTIVE', 'SUSPENDED')),
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- One row per mail domain, and the domain is the primary key.
--
-- A separate table rather than a comma-separated column on tenants, for the
-- one reason that decides it: **a domain must belong to exactly one company**.
-- A list inside a column cannot be constrained that way, and the failure it
-- allows is not a tidiness problem — a second tenant claiming
-- aaaindustryinc.com would have that company's employees' activation mail
-- routed to it.
--
-- Making the domain the key also makes the lookup that runs on every single
-- login — domain to tenant — a primary-key hit rather than a scan.
--
-- Multiple rows per tenant is the normal case, not an edge one: a Chinese
-- exporter commonly holds both example.com and example.com.cn.
CREATE TABLE tenant_domains (
    -- 253 is the maximum length of a DNS name.
    domain     VARCHAR(253) PRIMARY KEY,
    tenant_id  BIGINT       NOT NULL REFERENCES tenants(id),
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX tenant_domains_tenant_idx ON tenant_domains (tenant_id);

-- When the mailbox was proved to exist.
--
-- This one timestamp is the whole mechanism behind "only somebody the company
-- gave a mailbox can log in". It is set by exactly one thing: opening the
-- one-time link in a mail we sent to that address. So an address the company
-- never created cannot be activated, and an address typed wrong by an
-- administrator hard-bounces instead of quietly becoming a working account.
--
-- Nullable on purpose. NULL is a real state with a name — 待激活 — and the
-- employee page shows it, because during a migration the useful question is
-- not "is everyone in" but "who is stuck and where".
ALTER TABLE employees ADD COLUMN email_verified_at TIMESTAMPTZ;

-- The address is the login identifier, so it has to be unique within the
-- company — but it is deliberately NOT the primary key, which is where
-- ERPNext's model hurts: there, changing your email renames the record.
-- Here it is an ordinary mutable column and a change is an UPDATE.
--
-- Partial, because employees who never log in (a warehouse hand with no
-- company mailbox) keep the existing empty-string default, and '' cannot be
-- unique across all of them. lower() because an address is case-insensitive
-- in practice and " Alice@" must not buy a second identity.
CREATE UNIQUE INDEX employees_email_key
    ON employees (tenant_id, lower(email))
    WHERE email <> '';

-- +goose Down
DROP INDEX IF EXISTS employees_email_key;
ALTER TABLE employees DROP COLUMN IF EXISTS email_verified_at;
DROP TABLE IF EXISTS tenant_domains;
DROP TABLE IF EXISTS tenants;

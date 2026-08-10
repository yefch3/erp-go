-- +goose Up

-- Which company somebody belongs to stops being a property of their email
-- domain and becomes a property of their account.
--
-- The old rule was: the domain after the @ names the tenant. tenant_domains
-- has domain as its PRIMARY KEY, so a domain belongs to at most one company —
-- which is exactly right for aaaindustryinc.com and exactly wrong for
-- gmail.com, qq.com or 163.com. Not merely "the wrong company gets it": the
-- second company to try is refused by the primary key and cannot be onboarded
-- at all. Plenty of small exporters run their whole business on QQ mail.
--
-- The domain was doing two jobs. Deciding *which company* (it cannot, for a
-- public domain) and deciding *whether an address is a company mailbox* (it
-- does this well). Only the first job moves; IsTenantDomain and
-- ListTenantDomains keep doing the second, unchanged, when employees are
-- imported and invited.
--
-- Making the address globally unique is what lets login skip the domain
-- entirely: an address now identifies exactly one account across the whole
-- system, so there is nothing to disambiguate and no "which company?" step.
-- The cost is that one person cannot hold accounts in two tenants under the
-- same address, which for an ERP is the right trade — somebody working for
-- two companies has two work mailboxes.

-- +goose StatementBegin
-- Refuse loudly rather than pick a winner. If two tenants really do share an
-- address today, silently dropping one of them into a failed unique index —
-- or worse, letting the migration decide which account survives — is not a
-- decision a schema change gets to make.
DO $$
DECLARE clashes text;
BEGIN
    SELECT string_agg(addr, ', ') INTO clashes
    FROM (
        SELECT lower(email) AS addr
        FROM employees
        WHERE email <> ''
        GROUP BY lower(email)
        HAVING count(*) > 1
    ) d;
    IF clashes IS NOT NULL THEN
        RAISE EXCEPTION
            'these addresses exist in more than one tenant and must be resolved by hand first: %',
            clashes;
    END IF;
END $$;
-- +goose StatementEnd

DROP INDEX IF EXISTS employees_email_key;

-- Globally unique now, not per tenant. Still partial: employees who never log
-- in keep the empty-string default, and '' cannot be unique across them.
CREATE UNIQUE INDEX employees_email_key
    ON employees (lower(email))
    WHERE email <> '';

-- +goose Down
DROP INDEX IF EXISTS employees_email_key;
CREATE UNIQUE INDEX employees_email_key
    ON employees (tenant_id, lower(email))
    WHERE email <> '';

-- +goose Up

-- Make the address-book search indexable.
--
-- The composer searches with ILIKE '%keyword%'. A leading wildcard rules out
-- any btree index, so the planner was applying the match as a join filter:
-- every contact × active-customer pair materialised, then tested one by one.
-- Fine at five contacts, quadratic at fifty thousand — and it ran on every
-- keystroke.
--
-- Trigram indexes cut each string into three-character grams and index those,
-- which is what makes an infix match indexable at all. Prefix-only structures
-- (a trie, say) would be faster still for "kla…" but could never answer
-- "rhein" → klaus@rheinhandel.de, and searching by a fragment of the company
-- domain is exactly what people do with an address book.
--
-- pg_trgm is already in use for product recall (§5.6.16), so this adds an
-- index, not a dependency.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX customer_contacts_name_trgm_idx
    ON customer_contacts USING gin (name gin_trgm_ops);
CREATE INDEX customer_contacts_email_trgm_idx
    ON customer_contacts USING gin (email gin_trgm_ops);
CREATE INDEX customers_name_trgm_idx
    ON customers USING gin (name gin_trgm_ops);

-- +goose Down
DROP INDEX IF EXISTS customers_name_trgm_idx;
DROP INDEX IF EXISTS customer_contacts_email_trgm_idx;
DROP INDEX IF EXISTS customer_contacts_name_trgm_idx;

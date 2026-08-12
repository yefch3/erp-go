-- +goose NO TRANSACTION
-- +goose Up

-- Undoes 00032, which optimised a query nothing calls.
--
-- 00032 added an expression index on (tenant_id, owner_id,
-- coalesce(sent_at, received_at) DESC) because the ListInbound SQL sorts by
-- that expression and the existing index could not serve the sort. The
-- measurement was real - 16 ms to 0.06 ms on a 50k-message mailbox - and the
-- query is dead code. The inbox is drawn by ListInboundThreads, which sorts by
-- received_at, and the flat per-message list was never wired up. Both dead
-- queries go in the same change as this index.
--
-- What is left is a duplicate: the surviving email_inbound_owner_idx has the
-- same (tenant_id, owner_id) prefix and serves the filtering half identically,
-- while its received_at ordering is the one SearchMail actually pages by. An
-- index nobody reads is not free - every insert and update maintains it, it
-- takes 19 MB per 500k messages, and the next person to read the schema has to
-- work out why it is there.
--
-- Bring it back the day a per-message list view exists, and not before.
DROP INDEX CONCURRENTLY IF EXISTS email_inbound_owner_recent_idx;

-- +goose Down
CREATE INDEX CONCURRENTLY IF NOT EXISTS email_inbound_owner_recent_idx
    ON email_inbound (tenant_id, owner_id, (coalesce(sent_at, received_at)) DESC);

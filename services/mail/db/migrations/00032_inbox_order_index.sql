-- +goose NO TRANSACTION
-- +goose Up

-- The index the inbox list actually sorts by.
--
-- email_inbound_owner_idx is (tenant_id, owner_id, received_at DESC), and it
-- has been serving the filter half of every inbox query correctly. What it
-- cannot serve is the sort, because the list does not order by received_at:
--
--     ORDER BY coalesce(sent_at, received_at) DESC
--
-- The two differ deliberately. received_at is when we pulled the message down,
-- which for a backfill is "all at once, last Tuesday"; sent_at is when the
-- customer actually wrote, which is the order a person expects to read in.
-- But an index on one expression cannot order by another, so Postgres fell
-- back to reading every message the owner has and top-N sorting it to find
-- twenty-five.
--
-- That is invisible while a mailbox is small and gets steadily worse as it
-- fills, which is the worst shape a performance bug can have: nothing to see
-- during a pilot, and by the time it hurts, the person it hurts most is the
-- salesperson with three years of history - the most valuable user there is.
--
-- Measured on 500k messages with one owner holding 50k of them:
--
--     before   16.0 ms   Bitmap Index Scan -> 50,000 rows -> top-N heapsort
--     after     0.06 ms   Index Scan -> 25 rows
--
-- The old index stays. It looks redundant - same leading columns, and the new
-- one is smaller because coalesce collapses two columns into one key - but
-- SearchMail pages by keyset on (received_at, id) and orders by received_at
-- itself, so it is the one query that could still want the old shape. Proving
-- it does not is a separate exercise from fixing the inbox, and bundling an
-- unproven DROP with a measured CREATE would put both at risk on the same
-- migration. 19 MB per 500k messages is a cheap thing to be wrong about.
--
-- CONCURRENTLY because the poller writes to this table continuously; a plain
-- CREATE INDEX takes a lock that would stall every mailbox sync in flight for
-- as long as the build takes.
CREATE INDEX CONCURRENTLY IF NOT EXISTS email_inbound_owner_recent_idx
    ON email_inbound (tenant_id, owner_id, (coalesce(sent_at, received_at)) DESC);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS email_inbound_owner_recent_idx;

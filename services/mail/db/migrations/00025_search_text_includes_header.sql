-- +goose Up
-- Rebuild search_text now that it also holds the subject and the addresses.
--
-- 00023 indexed the body and left the subject and the sender to the ILIKE
-- predicates that were already there, ORed alongside. That works and is
-- slow, for a reason the plan makes plain: an OR across five columns cannot
-- use the trigram index, so Postgres reads every row.
--
--   five columns ORed   →  Seq Scan,          100 ms
--   search_text alone   →  Bitmap Index Scan, 1.6 ms
--
-- Measured on this mailbox — 2492 messages, 12 MB of text, a 16 MB index —
-- for a phrase that matches one mail. Sixty times, and the gap widens with
-- the mailbox, because one side of it is a scan.
--
-- So the header is folded into the same column and the query became a single
-- predicate. That is what a search document is: one field holding everything
-- the query may match, rather than the record's own shape.
--
-- Emptying rather than appending in SQL, for the reason 00023 gives for
-- having no SQL backfill: the service re-derives with the same function it
-- uses at ingest, so every row is built by one implementation. Every row is
-- cleared, not only the wrong-looking ones, because the composition changed
-- for all of them.
UPDATE email_inbound SET search_text = '' WHERE search_text <> '';

-- +goose Down
-- Nothing to undo. The column is derived; the down of 00023 drops it.
SELECT 1;

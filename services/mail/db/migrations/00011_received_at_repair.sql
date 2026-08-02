-- +goose Up

-- received_at used to be stamped now() on insert, so it recorded when the row
-- was written rather than when the mail arrived. Ordinarily the two are within
-- seconds of each other and nobody notices. A resync is where it shows: when
-- UIDVALIDITY changes the mailbox is re-fetched from scratch, every row is
-- rewritten, and a whole inbox's worth of 收到时间 collapses onto the same
-- minute — while the list, which sorted by the sender's Date header, kept a
-- different order entirely. Column and sort disagreed, and the column was the
-- one telling the lie.
--
-- Ingest now stores the host's own arrival time (IMAP INTERNALDATE). This
-- repairs what the old behaviour clobbered, using the Date header as the best
-- available stand-in for rows whose true arrival time was never recorded.
--
-- Only rows where the gap is large enough to be a rewrite rather than normal
-- delivery latency: an hour is far longer than any real fetch delay and far
-- shorter than the days a resync shifts things by, so correctly stored rows
-- are left exactly as they are.
UPDATE email_inbound
SET received_at = sent_at
WHERE sent_at IS NOT NULL
  AND received_at > sent_at + interval '1 hour';

-- +goose Down
-- Irreversible by nature: the timestamps this replaced were themselves
-- wrong, and the originals are not recoverable. Down is a no-op rather than
-- a restore that would put bad data back.
SELECT 1;

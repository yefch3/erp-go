-- +goose Up
-- The body, as plain text, so it can be searched.
--
-- Search covered the subject and the sender and not one word of what the mail
-- actually said. Adding the body raises two problems that have to be settled
-- before a single query is written.
--
-- One: body_text cannot be searched directly. In this mailbox 682 of 2482
-- messages have an empty body_text and a full body_html — bulk senders who
-- ship HTML with no text/plain alternative. Searching body_text would quietly
-- miss a quarter of the mail, and a search that finds nothing reads as "that
-- message does not exist", which is worse than having no search at all. Nor
-- can body_html be searched instead: a query for "content" would match
-- class="content", and every mail with an embedded image would match a query
-- for half the base64 alphabet. So the text is derived once, on the way in,
-- by the same HTMLToText the snippet already uses — proven on exactly these
-- 682 messages, whose snippets it produced.
--
-- Two: the index cannot be Postgres's own full-text search, because this
-- mailbox is trilingual and to_tsvector does not segment Chinese. Measured
-- here, not assumed:
--
--     to_tsvector('simple','这是本月的报价单和合同附件')
--       → '这是本月的报价单和合同附件':1        one token, the whole sentence
--     … @@ plainto_tsquery('simple','报价')  → false
--
-- English tokenises and stems correctly; Chinese does not tokenise at all.
-- Doing it properly needs zhparser or pg_jieba, and pg_available_extensions
-- on this image lists neither — only btree_gin, pg_trgm and unaccent. Adding
-- one would mean building and maintaining a Postgres image with a C extension
-- and a dictionary in it.
--
-- pg_trgm needs no dictionary and treats all three languages alike, because
-- three characters is three characters. It also buys something Gmail's
-- word-index approach cannot do: substring matching, so 报价 finds 报价单 and
-- a fragment of an order number finds the order. What it gives up is stemming
-- and synonyms, which for a mailbox of a few thousand messages is the better
-- side of the trade.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

ALTER TABLE email_inbound
    ADD COLUMN IF NOT EXISTS search_text TEXT NOT NULL DEFAULT '';

-- Measured on this mailbox: 16 MB of text, a 14 MB index, and a query for a
-- rare three-character term drops from a 131 ms sequential scan to 0.011 ms.
-- Queries under three characters cannot use it and fall back to the scan —
-- which is what every query does today, so nothing gets slower. The scan cost
-- is bounded rather than open-ended: MAIL_SYNC_HISTORY caps how much history
-- each folder holds.
CREATE INDEX IF NOT EXISTS email_inbound_search_trgm
    ON email_inbound USING gin (search_text gin_trgm_ops);

-- No SQL backfill. The 682 rows that need it are exactly the ones where a
-- regexp approximation of HTMLToText would differ from the real thing, and
-- two implementations of "what does this mail say" drifting apart is how a
-- search ends up finding a message by one route and not another. The service
-- fills the column on startup using the same function it uses at ingest.

-- +goose Down
DROP INDEX IF EXISTS email_inbound_search_trgm;
ALTER TABLE email_inbound DROP COLUMN IF EXISTS search_text;

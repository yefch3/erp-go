-- +goose Up
-- Whether this message actually went out carrying an open-tracking pixel.
--
-- The screen needs to distinguish three things, and until now the data could
-- only tell two apart:
--
--   opened_at set    → something fetched the pixel
--   opened_at null   → …either nobody opened it, or nobody was watching
--
-- Only the first of those two says anything about the recipient. Reporting
-- "未读" for a mail that never carried a pixel would be the system inventing a
-- fact about a customer.
--
-- It cannot be derived after the fact. The pixel is injected at send time into
-- a copy of the body — deliberately, so what is stored stays what the person
-- wrote and a reopened draft is not polluted — which means the stored body
-- never contains it. And the two conditions that decide it (an HTML body, and
-- the service having a publicly reachable address) are one per-message and one
-- per-deployment, the second of which changes over time. So it is recorded.
ALTER TABLE email_messages
    ADD COLUMN IF NOT EXISTS tracked BOOLEAN NOT NULL DEFAULT FALSE;

-- Backfill from the only evidence history left behind: a recorded open proves
-- there was a pixel to fetch. Everything else stays false, which reads as
-- "we do not know it was watched" — the honest answer.
UPDATE email_messages SET tracked = TRUE WHERE opened_at IS NOT NULL;

-- +goose Down
ALTER TABLE email_messages DROP COLUMN IF EXISTS tracked;

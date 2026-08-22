-- +goose Up

-- Whether a send carries the open-tracking pixel, chosen per message by the
-- person writing it.
--
-- Until now every HTML send was tracked unconditionally. The pixel has a
-- real cost the sender may not want to pay: it is a hidden 1×1 image on a
-- low-reputation domain — one of the signals that put a test mail in the
-- spam folder on 2026-08-20 — and the first mail to a new customer is
-- exactly the one that most needs deliverability and least needs tracking.
--
-- Default TRUE on both tables: the switch changes who decides, not what
-- happens when nobody does.
ALTER TABLE email_messages ADD COLUMN track_opens BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE email_drafts   ADD COLUMN track_opens BOOLEAN NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE email_drafts   DROP COLUMN track_opens;
ALTER TABLE email_messages DROP COLUMN track_opens;

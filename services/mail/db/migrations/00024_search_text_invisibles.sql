-- +goose Up
-- Search text that still holds characters which are in the mail without being
-- in its words.
--
-- Bulk senders pad their layout with soft hyphens and zero-width joiners.
-- They are invisible in a rendered mail and were mostly harmless in a snippet,
-- but the body is now indexed, and there they do real damage: a phrase with a
-- zero-width joiner sitting inside it does not match a query for that phrase,
-- so a mail becomes unfindable by words the reader can plainly see. One such
-- message answered a search for "online pharmacy" with a match snippet that
-- was two words followed by eighty soft hyphens.
--
-- U+00AD and U+2060 were simply missing from the strip list, and the list was
-- only ever applied on the HTML path — a text/plain body went in untouched.
-- Both are fixed in searchTextOf; this clears what was derived before, and
-- the service's startup backfill re-derives it with the same function it uses
-- at ingest. Emptying rather than repairing in SQL, for the reason 00023 gives
-- for having no SQL backfill at all: one implementation of "what does this
-- mail say", not two that drift.
--
-- Same shape as 00017's snippet repair: only rows that are actually wrong are
-- touched, so re-running costs nothing.
UPDATE email_inbound
SET search_text = ''
WHERE search_text ~ U&'[\00AD\034F\200B\200C\200D\2060\FEFF]';

-- +goose Down
-- Nothing to undo. The old value was the same text with invisible characters
-- in it, and keeping a copy of that to restore would be keeping rubbish on
-- purpose.
SELECT 1;

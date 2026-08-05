-- +goose Up
-- Link the replies that arrived before there was anywhere to put the link.
--
-- Ingest now carries the customer from the message a reply answers onto the
-- reply itself, but only for mail arriving from here on. Everything already
-- stored kept its zero. The rule is a join and nothing more — no parsing, no
-- guessing at addresses — so unlike the search text in 00023 it can be done
-- here without a second implementation to drift from the first.
--
-- Both header fields are followed, matching what resolveThread does: In-Reply-To
-- names the message being answered, References carries the whole chain, and a
-- client that fills in only the second is common enough to matter. Our
-- Message-IDs are <uuid@domain> and the stored parent key is the uuid, so the
-- comparison is on the part before the @.
UPDATE email_inbound i
SET customer_id   = m.customer_id,
    contact_id    = m.contact_id,
    customer_name = m.customer_name
FROM email_messages m
WHERE i.tenant_id = m.tenant_id
  AND i.customer_id = 0
  AND m.customer_id <> 0
  AND m.message_key::text = ANY (
      SELECT split_part(btrim(ref, '<>'), '@', 1)
      FROM unnest(
          array_remove(
              string_to_array(i.in_reply_to || ' ' || i.references_ids, ' '),
              '')
      ) AS ref
  );

-- +goose Down
-- Nothing to undo: the column's own down migration removes it, and the
-- previous value was the absence of an answer.
SELECT 1;

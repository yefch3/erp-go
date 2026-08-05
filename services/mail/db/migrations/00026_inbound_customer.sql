-- +goose Up
-- Which customer a received mail belongs to.
--
-- Mail we send has carried this since the beginning, because the composer
-- makes you pick a contact — at that moment the system knows who the mail is
-- about. Mail that comes back has carried nothing. A reply from a customer
-- landed in a table whose only clue about who sent it was a text string in
-- from_email, joinable to nothing.
--
-- So half of every conversation was business data and the other half was an
-- island. "Show me everything we have said to this customer" could not be
-- answered, because the answer needs a column to join on and there wasn't one.
-- That gap is the difference between a mailbox that lives inside an ERP and a
-- mailbox that merely sits next to one.
--
-- Filled by following the mail's own thread rather than by guessing at the
-- sender's address. A customer's client puts In-Reply-To on their reply,
-- naming the message they answered; that message is ours, and it knows its
-- customer. So the reply inherits it — no address matching, no contact list
-- lookup, and it holds even when the customer answers from an address nobody
-- has ever seen. Matching by address is a later and weaker layer for mail that
-- starts a conversation rather than continuing one.
--
-- Zero means unlinked, and unlinked is the ordinary case rather than a
-- failure: most of what arrives in a mailbox is not from a customer at all.
-- Nothing here creates a customer record for an unknown sender — a mailbox of
-- 2492 messages in development contains 198 from LinkedIn, and auto-creating
-- would have produced 198 customers named LinkedIn.
ALTER TABLE email_inbound
    ADD COLUMN IF NOT EXISTS customer_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS contact_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS customer_name VARCHAR(200) NOT NULL DEFAULT '';

-- The question this column exists to answer is always "this customer's mail",
-- so the index carries the customer first and the ordering after it.
CREATE INDEX IF NOT EXISTS email_inbound_customer_idx
    ON email_inbound (tenant_id, customer_id, received_at DESC)
    WHERE customer_id <> 0;

-- +goose Down
DROP INDEX IF EXISTS email_inbound_customer_idx;
ALTER TABLE email_inbound
    DROP COLUMN IF EXISTS customer_id,
    DROP COLUMN IF EXISTS contact_id,
    DROP COLUMN IF EXISTS customer_name;

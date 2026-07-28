-- +goose Up
-- Who the quotation is addressed to. The name and address are snapshotted
-- next to the id for the same reason as the customer name: a contact who
-- leaves the buyer must not blank out where a past offer went.
ALTER TABLE quotations
    ADD COLUMN contact_id    BIGINT,
    ADD COLUMN contact_name  VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN contact_email VARCHAR(200) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE quotations
    DROP COLUMN contact_email,
    DROP COLUMN contact_name,
    DROP COLUMN contact_id;

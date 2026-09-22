-- +goose Up
ALTER TABLE customer_contacts ADD COLUMN additional_emails TEXT[] NOT NULL DEFAULT '{}';

-- The gateway verifies access to the source mail before reading or saving a
-- selection; customer ownership is independently enforced in masterdata.
CREATE TABLE customer_mail_links (
    tenant_id BIGINT NOT NULL,
    inbound_id BIGINT NOT NULL,
    customer_id BIGINT NOT NULL REFERENCES customers(id),
    contact_id BIGINT NOT NULL REFERENCES customer_contacts(id),
    email TEXT NOT NULL,
    created_by BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, inbound_id)
);

-- +goose Down
DROP TABLE customer_mail_links;
ALTER TABLE customer_contacts DROP COLUMN additional_emails;

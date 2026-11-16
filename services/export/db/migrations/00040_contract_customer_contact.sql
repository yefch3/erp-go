-- +goose Up
-- A nullable snapshot distinguishes legacy contracts (use the accepted quote's
-- contact) from a new draft whose contact was explicitly left blank.
ALTER TABLE contract_versions ADD COLUMN customer_contact TEXT;

-- +goose Down
-- Preserve entered contact snapshots when rolling back application binaries.
SELECT 1;

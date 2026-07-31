-- +goose Up

-- Where a file came from, which is a different question from what it is.
--
-- kind='SIGNED' says "this is the countersigned copy". source says who put it
-- there: PLATFORM means an e-signature provider sent it through the webhook,
-- MANUAL means a person uploaded a scan. Both are legitimate - plenty of
-- export customers still print, stamp and email - but they carry very
-- different evidential weight, so the two must never be indistinguishable.
--
-- Only the webhook handler may write PLATFORM. The REST upload route
-- hardcodes MANUAL and does not read a source from the request at all, so
-- this is enforced by code rather than by convention.
ALTER TABLE contract_attachments
    ADD COLUMN source VARCHAR(16) NOT NULL DEFAULT 'MANUAL'
        CHECK (source IN ('PLATFORM', 'MANUAL'));

-- How the signature on this contract was established. NULL until it is
-- signed; after that it is the audit answer to "why do we believe the
-- customer agreed to this".
ALTER TABLE contracts
    ADD COLUMN signature_source VARCHAR(16) NOT NULL DEFAULT ''
        CHECK (signature_source IN ('', 'PLATFORM', 'MANUAL'));

-- +goose Down
ALTER TABLE contracts DROP COLUMN signature_source;
ALTER TABLE contract_attachments DROP COLUMN source;

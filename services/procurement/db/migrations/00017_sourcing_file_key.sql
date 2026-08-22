-- +goose Up

-- The customer's original inquiry Excel moves to object storage. The BYTEA
-- column was the honest choice when procurement had no file adapter — the
-- evidence had to live SOMEWHERE — but a database is a bad warehouse for
-- blobs: every base backup carries them, and they enjoy none of the
-- bucket's lifecycle rules.
--
-- The old column stays for rows written before this change; new rows store
-- a key here and NULL bytes there. Nothing has ever read the bytes back
-- (the query existed, its callers never did), so no dual-read fallback is
-- needed — the download path being added alongside reads keys only.
ALTER TABLE sourcing_cases
    ADD COLUMN source_file_key VARCHAR(500) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sourcing_cases DROP COLUMN source_file_key;

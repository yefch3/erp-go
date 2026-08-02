-- +goose Up

-- A mailbox can now be bound two ways.
--
-- PASSWORD is the traditional door: an authorisation code typed by the owner,
-- sealed in secret_enc. OAUTH is the modern one: the owner signs in on
-- Google's own page and what we hold is a refresh token — a revocable grant,
-- never the password, which we never see at all.
ALTER TABLE mail_accounts ADD COLUMN auth_kind VARCHAR(16) NOT NULL DEFAULT 'PASSWORD'
    CHECK (auth_kind IN ('PASSWORD','OAUTH'));

-- The refresh token, sealed under the same key as secret_enc but with its own
-- AAD, so a blob from one column cannot be replayed into the other.
ALTER TABLE mail_accounts ADD COLUMN oauth_refresh_enc BYTEA NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE mail_accounts DROP COLUMN oauth_refresh_enc;
ALTER TABLE mail_accounts DROP COLUMN auth_kind;

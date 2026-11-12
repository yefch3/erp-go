-- +goose Up
-- Keep quote bodies and their attachments as audit history while removing
-- erroneous quotes from the current procurement and sales workspaces.
ALTER TABLE inquiry_quotes ADD COLUMN deleted_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE inquiry_quotes DROP COLUMN deleted_at;

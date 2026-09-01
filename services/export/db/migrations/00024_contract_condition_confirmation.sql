-- +goose Up
-- migration-safety: nullable audit columns are backward compatible with contracts created by the previous release.
ALTER TABLE contracts ADD COLUMN condition_confirmed_at TIMESTAMPTZ;
ALTER TABLE contracts ADD COLUMN condition_confirmation_note TEXT NOT NULL DEFAULT '';
ALTER TABLE contracts ADD COLUMN condition_confirmed_by BIGINT;
ALTER TABLE contracts ADD COLUMN condition_confirmed_by_name VARCHAR(200) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE contracts DROP COLUMN condition_confirmed_by_name;
ALTER TABLE contracts DROP COLUMN condition_confirmed_by;
ALTER TABLE contracts DROP COLUMN condition_confirmation_note;
ALTER TABLE contracts DROP COLUMN condition_confirmed_at;

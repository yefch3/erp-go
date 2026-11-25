-- +goose Up
-- migration-safety: widening this string column preserves existing contract snapshots.
ALTER TABLE contract_versions ALTER COLUMN incoterm TYPE TEXT;

-- +goose Down
-- Older binaries read TEXT. Keep custom terms intact on rollback.
SELECT 1;

-- +goose Up
ALTER TABLE sourcing_lines
  ADD COLUMN decided_by BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN decided_by_name VARCHAR(150) NOT NULL DEFAULT '',
  ADD COLUMN decided_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE sourcing_lines
  DROP COLUMN decided_at,
  DROP COLUMN decided_by_name,
  DROP COLUMN decided_by;

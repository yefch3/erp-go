-- +goose Up
ALTER TABLE contracts DROP CONSTRAINT contracts_entry_source_check;
ALTER TABLE contracts ADD CONSTRAINT contracts_entry_source_check CHECK(entry_source IN ('STANDARD','EXISTING_CONTRACT','HISTORICAL_RECORD'));
-- +goose Down
-- Preserve records rather than converting them into executable contracts.
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM contracts WHERE entry_source='HISTORICAL_RECORD') THEN
  RAISE EXCEPTION 'Historical records exist; cannot remove record-only support';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE contracts DROP CONSTRAINT contracts_entry_source_check;
ALTER TABLE contracts ADD CONSTRAINT contracts_entry_source_check CHECK(entry_source IN ('STANDARD','EXISTING_CONTRACT'));

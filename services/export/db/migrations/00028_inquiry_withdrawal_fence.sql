-- +goose Up
CREATE TABLE inquiry_withdrawal_fences (
  tenant_id BIGINT NOT NULL, case_id BIGINT NOT NULL,
  PRIMARY KEY(tenant_id,case_id)
);
-- +goose StatementBegin
CREATE FUNCTION guard_inquiry_quotation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.source_sourcing_case_id IS NOT NULL THEN
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text || ':inquiry:' || NEW.source_sourcing_case_id::text,0));
    IF EXISTS(SELECT 1 FROM inquiry_withdrawal_fences WHERE tenant_id=NEW.tenant_id AND case_id=NEW.source_sourcing_case_id) THEN
      RAISE EXCEPTION 'inquiry withdrawn' USING ERRCODE='23514';
    END IF;
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER inquiry_quotation_fence BEFORE INSERT OR UPDATE ON quotations FOR EACH ROW EXECUTE FUNCTION guard_inquiry_quotation();
-- +goose StatementBegin
CREATE FUNCTION guard_inquiry_contract() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE source_id BIGINT;
BEGIN
  SELECT source_sourcing_case_id INTO source_id FROM quotations WHERE tenant_id=NEW.tenant_id AND id=NEW.quotation_id;
  IF source_id IS NOT NULL THEN
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text || ':inquiry:' || source_id::text,0));
    IF EXISTS(SELECT 1 FROM inquiry_withdrawal_fences WHERE tenant_id=NEW.tenant_id AND case_id=source_id) THEN
      RAISE EXCEPTION 'inquiry withdrawn' USING ERRCODE='23514';
    END IF;
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER inquiry_contract_fence BEFORE INSERT OR UPDATE OF quotation_id ON contracts FOR EACH ROW EXECUTE FUNCTION guard_inquiry_contract();
-- +goose Down
DROP TRIGGER inquiry_contract_fence ON contracts;
DROP FUNCTION guard_inquiry_contract();
DROP TRIGGER inquiry_quotation_fence ON quotations;
DROP FUNCTION guard_inquiry_quotation();
DROP TABLE inquiry_withdrawal_fences;

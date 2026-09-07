-- +goose Up
-- Customer confirmation and withdrawal must serialize on the source row even
-- when an older confirmation client is still open. No D2 business flow changes.
-- +goose StatementBegin
CREATE FUNCTION guard_inquiry_customer_confirmation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE source_status text; source_handoff text;
BEGIN
 IF NEW.status='CUSTOMER_CONFIRMED' THEN
  SELECT status,handoff_status INTO source_status,source_handoff FROM sourcing_cases
   WHERE tenant_id=NEW.tenant_id AND id=NEW.case_id FOR UPDATE;
  IF NOT FOUND OR source_status IN ('INTAKE_PENDING','CANCELLED') OR source_handoff='SALES_WITHDRAWN' THEN
   RAISE EXCEPTION 'inquiry withdrawn' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER inquiry_customer_confirmation_guard BEFORE INSERT OR UPDATE ON sourcing_customer_selections
 FOR EACH ROW EXECUTE FUNCTION guard_inquiry_customer_confirmation();
-- +goose Down
DROP TRIGGER inquiry_customer_confirmation_guard ON sourcing_customer_selections;
DROP FUNCTION guard_inquiry_customer_confirmation();

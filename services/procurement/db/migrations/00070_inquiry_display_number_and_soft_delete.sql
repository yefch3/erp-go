-- +goose Up

ALTER TABLE sourcing_cases
  ADD COLUMN display_inquiry_no VARCHAR(20) NOT NULL DEFAULT '',
  ADD COLUMN deleted_at TIMESTAMPTZ,
  ADD COLUMN deleted_by_id BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN deleted_by_name VARCHAR(150) NOT NULL DEFAULT '';

-- Existing case numbers remain untouched.  Give every existing inquiry a
-- deterministic display number, ordered within the date encoded in case_no.
WITH dated AS (
  SELECT id,
         tenant_id,
         CASE
           WHEN case_no ~ '^SC-[0-9]{8}-' THEN to_date(substring(case_no FROM 4 FOR 8), 'YYYYMMDD')
           ELSE (created_at AT TIME ZONE 'UTC')::date
         END AS inquiry_day
  FROM sourcing_cases
), ranked AS (
  SELECT id,
         inquiry_day,
         row_number() OVER (PARTITION BY tenant_id, inquiry_day ORDER BY id) AS daily_no
  FROM dated
)
UPDATE sourcing_cases c
SET display_inquiry_no = 'INQ-' || to_char(r.inquiry_day, 'YYMMDD') || '-' || lpad(r.daily_no::text, 3, '0')
FROM ranked r
WHERE r.id = c.id;

ALTER TABLE sourcing_cases
  ADD CONSTRAINT sourcing_cases_display_inquiry_no_format
  CHECK (display_inquiry_no ~ '^INQ-[0-9]{6}-[0-9]{3}$');

CREATE UNIQUE INDEX sourcing_cases_display_inquiry_no_unique
  ON sourcing_cases (tenant_id, display_inquiry_no);

-- Every insertion path (manual entry, upload and mailbox intake) receives the
-- same number.  The advisory transaction lock serializes allocation per
-- tenant/day, while the unique index is the final database guarantee.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION assign_sourcing_case_display_inquiry_no()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  inquiry_day date := current_date;
  prefix text := 'INQ-' || to_char(inquiry_day, 'YYMMDD') || '-';
  next_no integer;
BEGIN
  IF NEW.display_inquiry_no <> '' THEN
    RETURN NEW;
  END IF;

  PERFORM pg_advisory_xact_lock(hashtext(NEW.tenant_id::text), to_char(inquiry_day, 'YYYYMMDD')::integer);
  SELECT coalesce(max(right(display_inquiry_no, 3)::integer), 0) + 1
    INTO next_no
    FROM sourcing_cases
   WHERE tenant_id = NEW.tenant_id
     AND display_inquiry_no LIKE prefix || '%';

  IF next_no > 999 THEN
    RAISE EXCEPTION 'daily inquiry display number exhausted for tenant % on %', NEW.tenant_id, inquiry_day;
  END IF;
  NEW.display_inquiry_no := prefix || lpad(next_no::text, 3, '0');
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER sourcing_cases_assign_display_inquiry_no
BEFORE INSERT ON sourcing_cases
FOR EACH ROW EXECUTE FUNCTION assign_sourcing_case_display_inquiry_no();

-- +goose Down

DROP TRIGGER IF EXISTS sourcing_cases_assign_display_inquiry_no ON sourcing_cases;
DROP FUNCTION IF EXISTS assign_sourcing_case_display_inquiry_no();
DROP INDEX IF EXISTS sourcing_cases_display_inquiry_no_unique;
ALTER TABLE sourcing_cases
  DROP CONSTRAINT IF EXISTS sourcing_cases_display_inquiry_no_format,
  DROP COLUMN IF EXISTS deleted_by_name,
  DROP COLUMN IF EXISTS deleted_by_id,
  DROP COLUMN IF EXISTS deleted_at,
  DROP COLUMN IF EXISTS display_inquiry_no;

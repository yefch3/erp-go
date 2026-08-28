-- +goose Up
ALTER TABLE sourcing_cases
  ADD COLUMN handoff_status text NOT NULL DEFAULT 'DRAFT',
  ADD COLUMN requirement_version_no integer NOT NULL DEFAULT 0,
  ADD COLUMN accepted_by bigint,
  ADD COLUMN accepted_by_name text NOT NULL DEFAULT '',
  ADD COLUMN accepted_at timestamptz,
  ADD COLUMN returned_by bigint,
  ADD COLUMN returned_by_name text NOT NULL DEFAULT '',
  ADD COLUMN returned_at timestamptz,
  ADD COLUMN return_reason text NOT NULL DEFAULT '',
  ADD COLUMN return_fields text[] NOT NULL DEFAULT '{}';

UPDATE sourcing_cases
SET requirement_version_no = 1,
    handoff_status = CASE status
      WHEN 'INTAKE_PENDING' THEN 'DRAFT'
      WHEN 'REVIEWING' THEN 'WAITING_ACCEPTANCE'
      WHEN 'CUSTOMER_QUOTE_CREATED' THEN 'QUOTE_IN_PROGRESS'
      WHEN 'CANCELLED' THEN 'CANCELLED'
      ELSE 'IN_PROGRESS'
    END;

ALTER TABLE sourcing_cases
  ADD CONSTRAINT sourcing_cases_handoff_status_check CHECK (
    handoff_status IN ('DRAFT','WAITING_ACCEPTANCE','IN_PROGRESS','RETURNED_FOR_SUPPLEMENT',
                       'COST_CONFIRMED','SUBMITTED_TO_SALES','QUOTE_IN_PROGRESS','CANCELLED')
  ),
  ADD CONSTRAINT sourcing_cases_requirement_version_check CHECK (requirement_version_no >= 0);

ALTER TABLE cost_scenarios
  ADD COLUMN requirement_version_no integer NOT NULL DEFAULT 1,
  ADD COLUMN submitted_to_sales_by bigint,
  ADD COLUMN submitted_to_sales_by_name text NOT NULL DEFAULT '',
  ADD COLUMN submitted_to_sales_at timestamptz;

UPDATE cost_scenarios cs
SET requirement_version_no = greatest(sc.requirement_version_no, 1)
FROM sourcing_cases sc
WHERE sc.tenant_id = cs.tenant_id AND sc.id = cs.case_id;

-- +goose Down
ALTER TABLE cost_scenarios
  DROP COLUMN submitted_to_sales_at,
  DROP COLUMN submitted_to_sales_by_name,
  DROP COLUMN submitted_to_sales_by,
  DROP COLUMN requirement_version_no;

ALTER TABLE sourcing_cases
  DROP CONSTRAINT sourcing_cases_requirement_version_check,
  DROP CONSTRAINT sourcing_cases_handoff_status_check,
  DROP COLUMN return_fields,
  DROP COLUMN return_reason,
  DROP COLUMN returned_at,
  DROP COLUMN returned_by_name,
  DROP COLUMN returned_by,
  DROP COLUMN accepted_at,
  DROP COLUMN accepted_by_name,
  DROP COLUMN accepted_by,
  DROP COLUMN requirement_version_no,
  DROP COLUMN handoff_status;

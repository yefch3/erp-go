-- +goose Up
-- D7 travel reimbursement ledger. Approval instances remain in approval;
-- this service owns the claim, evidence and payment facts.
CREATE TABLE travel_reimbursements (
  id bigserial PRIMARY KEY,
  tenant_id bigint NOT NULL,
  claim_no text NOT NULL,
  claimant_id bigint NOT NULL,
  claimant_name text NOT NULL,
  department_name text NOT NULL DEFAULT '',
  trip_start date NOT NULL,
  trip_end date NOT NULL,
  origin text NOT NULL,
  destination text NOT NULL,
  purpose text NOT NULL,
  amount numeric(18,2) NOT NULL CHECK (amount > 0),
  currency varchar(3) NOT NULL,
  payment_account text NOT NULL,
  note text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT','PENDING_DEPARTMENT_CONFIRMATION','PENDING_FINANCE_APPROVAL','REJECTED','PENDING_PAYMENT','PAID')),
  approval_instance_id bigint,
  rejection_reason text NOT NULL DEFAULT '',
  paid_at date,
  paid_by bigint,
  paid_by_name text NOT NULL DEFAULT '',
  payment_reference text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, claim_no)
);
CREATE INDEX travel_reimbursements_tenant_claimant_idx ON travel_reimbursements (tenant_id, claimant_id, created_at DESC);
CREATE INDEX travel_reimbursements_tenant_status_idx ON travel_reimbursements (tenant_id, status, created_at DESC);

CREATE TABLE travel_reimbursement_files (
  id bigserial PRIMARY KEY,
  tenant_id bigint NOT NULL,
  reimbursement_id bigint NOT NULL REFERENCES travel_reimbursements(id),
  category text NOT NULL CHECK (category IN ('INVOICE','RECEIPT','ITINERARY','PAYMENT_PROOF','OTHER')),
  file_name text NOT NULL,
  object_key text NOT NULL,
  uploaded_by bigint NOT NULL,
  uploaded_by_name text NOT NULL,
  uploaded_at timestamptz NOT NULL DEFAULT now(),
  removed_at timestamptz,
  UNIQUE (tenant_id, object_key)
);
CREATE INDEX travel_reimbursement_files_parent_idx ON travel_reimbursement_files (tenant_id, reimbursement_id) WHERE removed_at IS NULL;

CREATE TABLE travel_reimbursement_history (
  id bigserial PRIMARY KEY,
  tenant_id bigint NOT NULL,
  reimbursement_id bigint NOT NULL REFERENCES travel_reimbursements(id),
  action text NOT NULL,
  from_status text NOT NULL DEFAULT '',
  to_status text NOT NULL DEFAULT '',
  detail text NOT NULL DEFAULT '',
  actor_id bigint NOT NULL,
  actor_name text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX travel_reimbursement_history_parent_idx ON travel_reimbursement_history (tenant_id, reimbursement_id, created_at, id);

-- +goose Down
DROP TABLE travel_reimbursement_history;
DROP TABLE travel_reimbursement_files;
DROP TABLE travel_reimbursements;


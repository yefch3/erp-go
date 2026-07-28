-- +goose Up

-- Until now "驳回" and "退回" did the same thing: both put the contract back
-- in the submitter's hands as an editable draft. That made 驳回 toothless —
-- the submitter could resubmit the identical document and start the same
-- round again.
--
-- From here the two mean different things:
--   退回 RETURN  → back to DRAFT, fix it and resubmit. Unchanged.
--   驳回 REJECT  → this deal is off. The contract lands in REJECTED, which
--                  can only be voided; the version is frozen at REJECTED so
--                  it cannot be edited or sent round again.
ALTER TABLE contracts DROP CONSTRAINT contracts_status_check;
ALTER TABLE contracts ADD CONSTRAINT contracts_status_check
    CHECK (status IN ('DRAFT','PENDING_APPROVAL','PENDING_SIGN','REJECTED','EFFECTIVE',
                      'EXECUTING','COMPLETED','CANCELLED'));

ALTER TABLE contract_versions DROP CONSTRAINT contract_versions_status_check;
ALTER TABLE contract_versions ADD CONSTRAINT contract_versions_status_check
    CHECK (status IN ('DRAFT','PENDING_APPROVAL','APPROVED','REJECTED','SUPERSEDED'));

-- +goose Down
ALTER TABLE contract_versions DROP CONSTRAINT contract_versions_status_check;
ALTER TABLE contract_versions ADD CONSTRAINT contract_versions_status_check
    CHECK (status IN ('DRAFT','PENDING_APPROVAL','APPROVED','SUPERSEDED'));

ALTER TABLE contracts DROP CONSTRAINT contracts_status_check;
ALTER TABLE contracts ADD CONSTRAINT contracts_status_check
    CHECK (status IN ('DRAFT','PENDING_APPROVAL','PENDING_SIGN','EFFECTIVE',
                      'EXECUTING','COMPLETED','CANCELLED'));

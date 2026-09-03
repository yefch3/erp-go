-- +goose Up

-- Existing contracts may arrive with a missing external number or a typing
-- error in their non-financial terms. Corrections are applied in place so
-- procurement/shipping keep their original contract item ids, while this
-- append-only row preserves exactly what changed and who changed it.
CREATE TABLE contract_corrections (
    id                  BIGSERIAL PRIMARY KEY,
    tenant_id           BIGINT      NOT NULL,
    contract_id         BIGINT      NOT NULL REFERENCES contracts(id),
    contract_version_id BIGINT      NOT NULL REFERENCES contract_versions(id),
    before_data         JSONB       NOT NULL,
    after_data          JSONB       NOT NULL,
    corrected_by        BIGINT      NOT NULL,
    corrected_by_name   VARCHAR(200) NOT NULL DEFAULT '',
    corrected_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX contract_corrections_contract_idx
    ON contract_corrections (tenant_id, contract_id, corrected_at DESC);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION contract_versions_freeze() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    old_body contract_versions%ROWTYPE := OLD;
    new_body contract_versions%ROWTYPE := NEW;
BEGIN
    -- Set only inside the audited correction transaction. Contract lines,
    -- totals, currency and FX are never included in the correction UPDATE.
    IF current_setting('erp.contract_correction', true) = 'on' THEN
        RETURN NEW;
    END IF;
    IF OLD.status NOT IN ('APPROVED', 'SUPERSEDED') THEN
        RETURN NEW;
    END IF;
    old_body.status := NULL;
    new_body.status := NULL;
    IF old_body IS DISTINCT FROM new_body THEN
        RAISE EXCEPTION 'contract version % is frozen (status %)', OLD.id, OLD.status
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF OLD.status = NEW.status OR (OLD.status = 'APPROVED' AND NEW.status = 'SUPERSEDED') THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'contract version % cannot move from % to %', OLD.id, OLD.status, NEW.status
        USING ERRCODE = 'restrict_violation';
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION contract_versions_freeze() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    old_body contract_versions%ROWTYPE := OLD;
    new_body contract_versions%ROWTYPE := NEW;
BEGIN
    IF OLD.status NOT IN ('APPROVED', 'SUPERSEDED') THEN
        RETURN NEW;
    END IF;
    old_body.status := NULL;
    new_body.status := NULL;
    IF old_body IS DISTINCT FROM new_body THEN
        RAISE EXCEPTION 'contract version % is frozen (status %)', OLD.id, OLD.status
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF OLD.status = NEW.status OR (OLD.status = 'APPROVED' AND NEW.status = 'SUPERSEDED') THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'contract version % cannot move from % to %', OLD.id, OLD.status, NEW.status
        USING ERRCODE = 'restrict_violation';
END;
$$;
-- +goose StatementEnd
DROP TABLE contract_corrections;

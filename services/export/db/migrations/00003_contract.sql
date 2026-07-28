-- +goose Up

-- A contract is a shell: identity, who it is with, and where it stands.
-- Everything negotiable lives in contract_versions, because the terms of a
-- signed contract must never be edited in place.
CREATE TABLE contracts (
    id                 BIGSERIAL     PRIMARY KEY,
    tenant_id          BIGINT        NOT NULL DEFAULT 1,
    contract_no        VARCHAR(50)   NOT NULL,
    -- The quotation this grew out of, kept for traceability. Null is allowed
    -- because not every contract starts life as an offer.
    quotation_id       BIGINT        REFERENCES quotations(id),
    quote_no           VARCHAR(50)   NOT NULL DEFAULT '',
    customer_id        BIGINT        NOT NULL,
    customer_name      VARCHAR(200)  NOT NULL DEFAULT '',
    -- The version currently IN FORCE, not the newest one. It stays where it
    -- is while a change version is drafted and approved, and only moves when
    -- the customer signs the new version. Null until the first signature.
    current_version_id BIGINT,
    status             VARCHAR(32)   NOT NULL DEFAULT 'DRAFT'
                       CHECK (status IN ('DRAFT','PENDING_APPROVAL','PENDING_SIGN','EFFECTIVE',
                                         'EXECUTING','COMPLETED','CANCELLED')),
    -- Where to return to if the approval comes back rejected. A first draft
    -- falls back to DRAFT; a change to a contract already running has to be
    -- put back exactly where it was, which the status alone cannot say.
    status_before_approval VARCHAR(32) NOT NULL DEFAULT '',
    sales_employee_id  BIGINT        NOT NULL DEFAULT 0,
    sales_employee     VARCHAR(100)  NOT NULL DEFAULT '',
    signed_at          TIMESTAMPTZ,
    effective_at       TIMESTAMPTZ,
    completed_at       TIMESTAMPTZ,
    created_at         TIMESTAMPTZ   NOT NULL DEFAULT now(),
    created_by         BIGINT        NOT NULL DEFAULT 0,
    updated_at         TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_by         BIGINT        NOT NULL DEFAULT 0,
    UNIQUE (tenant_id, contract_no)
);
CREATE INDEX contracts_customer_idx ON contracts (tenant_id, customer_id, created_at DESC);
-- One live contract per quotation. Cancelled ones are excluded so a deal that
-- fell through can be written up again from the same offer.
CREATE UNIQUE INDEX contracts_quotation_idx ON contracts (tenant_id, quotation_id)
    WHERE quotation_id IS NOT NULL AND status <> 'CANCELLED';

-- An immutable snapshot of the terms. A change is a new row, never an UPDATE.
CREATE TABLE contract_versions (
    id                BIGSERIAL     PRIMARY KEY,
    tenant_id         BIGINT        NOT NULL DEFAULT 1,
    contract_id       BIGINT        NOT NULL REFERENCES contracts(id),
    version_no        INT           NOT NULL,
    buyer_name        VARCHAR(200)  NOT NULL DEFAULT '',
    buyer_address     TEXT          NOT NULL DEFAULT '',
    seller_name       VARCHAR(200)  NOT NULL DEFAULT '',
    seller_address    TEXT          NOT NULL DEFAULT '',
    currency          CHAR(3)       NOT NULL,
    incoterm          VARCHAR(20)   NOT NULL DEFAULT 'FOB',
    port_of_loading   VARCHAR(100)  NOT NULL DEFAULT '',
    port_of_discharge VARCHAR(100)  NOT NULL DEFAULT '',
    payment_method    VARCHAR(50)   NOT NULL DEFAULT '',
    -- Required before the version may be submitted, not at creation: a
    -- contract generated from a quotation has no delivery date to copy.
    delivery_date     DATE,
    terms             TEXT          NOT NULL DEFAULT '',
    total_amount      NUMERIC(18,2) NOT NULL DEFAULT 0,
    base_amount       NUMERIC(18,2) NOT NULL DEFAULT 0,
    -- Inherited from the quotation, never re-read from fx: the rate promised
    -- when the offer was made is the rate the contract is priced at.
    fx_rate           NUMERIC(18,8) NOT NULL,
    fx_rate_at        TIMESTAMPTZ   NOT NULL,
    fx_source         VARCHAR(50)   NOT NULL,
    fx_base_currency  CHAR(3)       NOT NULL DEFAULT 'USD',
    -- Mandatory from version 2 onwards; enforced in the service, since the
    -- database cannot see which version this is without a subquery.
    change_reason     TEXT          NOT NULL DEFAULT '',
    status            VARCHAR(32)   NOT NULL DEFAULT 'DRAFT'
                      CHECK (status IN ('DRAFT','PENDING_APPROVAL','APPROVED','SUPERSEDED')),
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT now(),
    created_by        BIGINT        NOT NULL DEFAULT 0,
    UNIQUE (contract_id, version_no)
);
CREATE INDEX contract_versions_contract_idx ON contract_versions (tenant_id, contract_id, version_no DESC);

CREATE TABLE contract_items (
    id                  BIGSERIAL     PRIMARY KEY,
    tenant_id           BIGINT        NOT NULL DEFAULT 1,
    contract_version_id BIGINT        NOT NULL REFERENCES contract_versions(id) ON DELETE CASCADE,
    line_no             INT           NOT NULL,
    product_id          BIGINT        NOT NULL,
    sku_id              BIGINT,
    -- Snapshotted like the quotation's: a product renamed next year must not
    -- rewrite what this contract says was sold.
    product_code        VARCHAR(100)  NOT NULL DEFAULT '',
    product_name        VARCHAR(200)  NOT NULL DEFAULT '',
    spec                VARCHAR(500)  NOT NULL DEFAULT '',
    qty                 NUMERIC(18,4) NOT NULL CHECK (qty > 0),
    uom_id              BIGINT        NOT NULL DEFAULT 0,
    uom_code            VARCHAR(20)   NOT NULL DEFAULT '',
    unit_price          NUMERIC(18,4) NOT NULL DEFAULT 0,
    amount              NUMERIC(18,2) NOT NULL DEFAULT 0,
    hs_code             VARCHAR(50)   NOT NULL DEFAULT '',
    remark              TEXT          NOT NULL DEFAULT '',
    UNIQUE (contract_version_id, line_no)
);
CREATE INDEX contract_items_version_idx ON contract_items (tenant_id, contract_version_id);

-- +goose StatementBegin
-- Immutability is an invariant, so it is enforced where nothing can bypass
-- it. Application code that tries to edit an approved version fails loudly
-- rather than quietly rewriting history.
CREATE FUNCTION contract_versions_freeze() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    old_body contract_versions%ROWTYPE := OLD;
    new_body contract_versions%ROWTYPE := NEW;
BEGIN
    IF OLD.status NOT IN ('APPROVED', 'SUPERSEDED') THEN
        RETURN NEW;
    END IF;
    -- Retiring a version is the one change an approved row may still undergo,
    -- so compare everything except the status column.
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

CREATE TRIGGER contract_versions_freeze
    BEFORE UPDATE ON contract_versions
    FOR EACH ROW EXECUTE FUNCTION contract_versions_freeze();

-- +goose StatementBegin
-- The same protection for the lines: freezing the header would be theatre if
-- the amounts underneath it could still be edited.
CREATE FUNCTION contract_items_freeze() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    version_id BIGINT;
    parent_status TEXT;
BEGIN
    IF TG_OP = 'DELETE' THEN
        version_id := OLD.contract_version_id;
    ELSE
        version_id := NEW.contract_version_id;
    END IF;
    SELECT status INTO parent_status FROM contract_versions WHERE id = version_id;
    IF parent_status IN ('APPROVED', 'SUPERSEDED') THEN
        RAISE EXCEPTION 'contract version % is frozen; its lines cannot change', version_id
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER contract_items_freeze
    BEFORE INSERT OR UPDATE OR DELETE ON contract_items
    FOR EACH ROW EXECUTE FUNCTION contract_items_freeze();

-- +goose Down
DROP TRIGGER contract_items_freeze ON contract_items;
DROP FUNCTION contract_items_freeze();
DROP TRIGGER contract_versions_freeze ON contract_versions;
DROP FUNCTION contract_versions_freeze();
DROP TABLE contract_items;
DROP TABLE contract_versions;
DROP TABLE contracts;

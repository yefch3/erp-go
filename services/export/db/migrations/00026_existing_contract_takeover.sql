-- +goose Up

-- A manually entered, already signed contract is a real running agreement,
-- not a draft waiting to repeat approvals that happened outside the system.
ALTER TABLE contracts
    ADD COLUMN external_contract_no VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN entry_source VARCHAR(32) NOT NULL DEFAULT 'STANDARD'
        CHECK (entry_source IN ('STANDARD', 'EXISTING_CONTRACT')),
    ADD COLUMN opening_received_amount NUMERIC(18,2) NOT NULL DEFAULT 0
        CHECK (opening_received_amount >= 0),
    ADD COLUMN file_pending BOOLEAN NOT NULL DEFAULT FALSE;

CREATE UNIQUE INDEX contracts_external_no_idx
    ON contracts (tenant_id, external_contract_no)
    WHERE external_contract_no <> '' AND status <> 'CANCELLED';

-- Opening quantities are snapshots of work completed before this ERP took
-- over. They are deliberately not represented as fake POs, receipts or
-- outbounds. New operational documents only account for work done here.
ALTER TABLE contract_items
    ADD COLUMN opening_procured_qty NUMERIC(18,4) NOT NULL DEFAULT 0
        CHECK (opening_procured_qty >= 0 AND opening_procured_qty <= qty),
    ADD COLUMN opening_arrived_qty NUMERIC(18,4) NOT NULL DEFAULT 0
        CHECK (opening_arrived_qty >= 0 AND opening_arrived_qty <= qty),
    ADD COLUMN opening_shipped_qty NUMERIC(18,4) NOT NULL DEFAULT 0
        CHECK (opening_shipped_qty >= 0 AND opening_shipped_qty <= qty);

-- +goose Down
ALTER TABLE contract_items
    DROP COLUMN opening_shipped_qty,
    DROP COLUMN opening_arrived_qty,
    DROP COLUMN opening_procured_qty;
DROP INDEX contracts_external_no_idx;
ALTER TABLE contracts
    DROP COLUMN file_pending,
    DROP COLUMN opening_received_amount,
    DROP COLUMN entry_source,
    DROP COLUMN external_contract_no;

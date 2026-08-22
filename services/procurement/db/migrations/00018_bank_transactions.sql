-- +goose Up

-- The fifth voice in the reconciliation. Orders promise, receipts observe,
-- invoices claim, payments assert — and this table is what the BANK says
-- happened, imported from the statement CSV the finance clerk downloads
-- from the bank portal. supplier_payments kept two empty chairs for it
-- (bank_ref, bank_txn_id) since P3; today the guest arrives.
--
-- A statement row is the bank's record, not ours: rows are never edited or
-- deleted here, only imported and pointed at. Matching lives on the payment
-- side — supplier_payments.bank_txn_id → this table — so "which payment
-- does the bank confirm" reads in one join and un-matching touches only
-- our own assertion, never the bank's.
CREATE TABLE bank_transactions (
    id               BIGSERIAL     PRIMARY KEY,
    tenant_id        BIGINT        NOT NULL DEFAULT 1,

    txn_date         DATE          NOT NULL,
    -- DEBIT: money left the account (the side supplier payments live on).
    -- CREDIT: money arrived — imported for completeness, matched by nothing
    -- yet; customer receipts live in export's book.
    direction        VARCHAR(8)    NOT NULL CHECK (direction IN ('DEBIT','CREDIT')),
    amount           NUMERIC(18,2) NOT NULL CHECK (amount > 0),
    currency         VARCHAR(8)    NOT NULL,
    counterparty     VARCHAR(200)  NOT NULL DEFAULT '',

    -- The bank's own reference. The idempotency key: re-uploading last
    -- week's CSV (finance WILL do this) adds nothing and breaks nothing.
    bank_ref         VARCHAR(100)  NOT NULL,

    remark           TEXT          NOT NULL DEFAULT '',
    source_file      VARCHAR(200)  NOT NULL DEFAULT '',
    imported_by_id   BIGINT        NOT NULL DEFAULT 0,
    imported_by_name VARCHAR(100)  NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ   NOT NULL DEFAULT now(),

    UNIQUE (tenant_id, bank_ref)
);
CREATE INDEX bank_transactions_list_idx
    ON bank_transactions (tenant_id, txn_date DESC, id DESC);

-- One payment per bank row: without this, two payments could both claim the
-- same wire and the book would confirm more money than ever left.
CREATE UNIQUE INDEX supplier_payments_bank_txn_once
    ON supplier_payments (bank_txn_id) WHERE bank_txn_id IS NOT NULL;

-- +goose Down
DROP INDEX supplier_payments_bank_txn_once;
DROP TABLE bank_transactions;

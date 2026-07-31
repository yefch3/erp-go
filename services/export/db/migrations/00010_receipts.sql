-- +goose Up

-- Our own accounts. Kept for one specific reason: a transfer between two of
-- them (settling a foreign-currency account into the RMB one) shows up as a
-- large credit that looks exactly like a customer payment. Without a list of
-- what is ours, that money gets allocated to a contract and the books are
-- wrong in a way nobody notices for a month.
CREATE TABLE bank_accounts (
    id           BIGSERIAL    PRIMARY KEY,
    tenant_id    BIGINT       NOT NULL DEFAULT 1,
    account_no   VARCHAR(64)  NOT NULL,
    account_name VARCHAR(200) NOT NULL,
    bank_name    VARCHAR(200) NOT NULL DEFAULT '',
    currency     VARCHAR(8)   NOT NULL DEFAULT 'USD',
    status       VARCHAR(16)  NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, account_no)
);

-- What the bank says. This is fact, not judgement: amount, date and
-- counterparty come from the statement and are never edited afterwards.
-- Only the disposition — what we decided to do about it — changes.
--
-- Every line is stored, including the ones that have nothing to do with
-- receivables. Filtering at the door would make it impossible to ever explain
-- why the bank balance and the system disagree; reconciliation is about
-- completeness, so every line needs a resting place, even if that place is
-- "not ours to match".
CREATE TABLE bank_transactions (
    id              BIGSERIAL     PRIMARY KEY,
    tenant_id       BIGINT        NOT NULL DEFAULT 1,
    account_id      BIGINT        NOT NULL REFERENCES bank_accounts(id),
    -- The bank's own reference. This is the idempotency key: importing the
    -- same statement twice must not double the money.
    bank_ref        VARCHAR(100)  NOT NULL,
    direction       VARCHAR(8)    NOT NULL CHECK (direction IN ('CREDIT','DEBIT')),
    amount          NUMERIC(18,2) NOT NULL CHECK (amount > 0),
    currency        VARCHAR(8)    NOT NULL,
    -- The bank's date, not the day somebody typed it in.
    value_date      DATE          NOT NULL,
    counterparty    VARCHAR(200)  NOT NULL DEFAULT '',
    counterparty_account VARCHAR(100) NOT NULL DEFAULT '',
    -- MT103 field 70 or the domestic equivalent. Where a contract number
    -- shows up when the customer bothered to write one.
    remittance_info TEXT          NOT NULL DEFAULT '',
    -- How it got in. The matching logic below does not care, which is the
    -- point: CSV and API can be added later without touching any of it.
    source          VARCHAR(16)   NOT NULL DEFAULT 'MANUAL'
        CHECK (source IN ('MANUAL','CSV','API','PSP')),
    -- A reference we issued ourselves (payment link). Trustworthy in a way
    -- the customer-typed remittance_info is not, so only this one may drive
    -- an automatic allocation.
    trusted_ref     VARCHAR(100)  NOT NULL DEFAULT '',
    disposition     VARCHAR(20)   NOT NULL DEFAULT 'UNPROCESSED'
        CHECK (disposition IN ('UNPROCESSED','ALLOCATED','IRRELEVANT')),
    irrelevant_type VARCHAR(32)   NOT NULL DEFAULT ''
        CHECK (irrelevant_type IN ('','TAX_REFUND','INTEREST','INTERNAL','SUPPLIER_REFUND','DEPOSIT_RETURN','OTHER')),
    note            TEXT          NOT NULL DEFAULT '',
    recorded_by     BIGINT        NOT NULL DEFAULT 0,
    recorded_by_name VARCHAR(100) NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, account_id, bank_ref)
);
CREATE INDEX bank_tx_queue_idx ON bank_transactions (tenant_id, disposition, value_date DESC);
CREATE INDEX bank_tx_party_idx ON bank_transactions (tenant_id, counterparty);

-- What we decided the money was for. Append-only.
--
-- A correction is a reversal row (negative amount pointing at the original),
-- never a DELETE and never an UPDATE of the amount. Same rule the document
-- already sets for receipts: "已确认收汇不能删除，只能冲销". Summing the
-- table gives the current truth; reading it in order gives the history of how
-- somebody arrived at it.
CREATE TABLE receipt_allocations (
    id             BIGSERIAL     PRIMARY KEY,
    tenant_id      BIGINT        NOT NULL DEFAULT 1,
    transaction_id BIGINT        NOT NULL REFERENCES bank_transactions(id),
    contract_id    BIGINT        NOT NULL REFERENCES contracts(id),
    contract_no    VARCHAR(50)   NOT NULL,
    customer_name  VARCHAR(200)  NOT NULL DEFAULT '',
    -- Taken out of the bank line. Negative on a reversal row.
    amount         NUMERIC(18,2) NOT NULL CHECK (amount <> 0),
    -- The gap we absorb: intermediary bank charges deducted in transit. It
    -- does NOT come out of the bank line — the customer's 50,000 arrived as
    -- 49,975 and the contract is still settled in full. Kept as its own
    -- column so it can become a ledger expense later without re-deriving it.
    fee_amount     NUMERIC(18,2) NOT NULL DEFAULT 0,
    currency       VARCHAR(8)    NOT NULL,
    -- Set on a reversal row; points at the row being undone.
    reversal_of    BIGINT        REFERENCES receipt_allocations(id),
    reverse_reason TEXT          NOT NULL DEFAULT '',
    allocated_by   BIGINT        NOT NULL DEFAULT 0,
    allocated_by_name VARCHAR(100) NOT NULL DEFAULT '',
    allocated_at   TIMESTAMPTZ   NOT NULL DEFAULT now()
);
CREATE INDEX receipt_alloc_tx_idx       ON receipt_allocations (tenant_id, transaction_id);
CREATE INDEX receipt_alloc_contract_idx ON receipt_allocations (tenant_id, contract_id);
-- One reversal per row. Without this, two clicks on 冲销 undo the same
-- allocation twice and the contract's balance goes up by double.
CREATE UNIQUE INDEX receipt_alloc_reversal_once
    ON receipt_allocations (reversal_of) WHERE reversal_of IS NOT NULL;

-- +goose Down
DROP TABLE receipt_allocations;
DROP TABLE bank_transactions;
DROP TABLE bank_accounts;

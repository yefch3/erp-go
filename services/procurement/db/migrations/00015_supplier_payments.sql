-- +goose Up

-- Money leaving for a supplier. The fourth set of numbers in the
-- reconciliation: order (our promise), receipt (what we saw), invoice (their
-- claim), and now this — what we assert we paid. The bank statement is a
-- fifth voice and deliberately NOT this table; bank_ref/bank_txn_id below
-- are the empty chairs kept for it, so folding statements in later is a
-- backfill, not a schema change.
CREATE TABLE supplier_payments (
    id              BIGSERIAL     PRIMARY KEY,
    tenant_id       BIGINT        NOT NULL DEFAULT 1,
    supplier_id     BIGINT        NOT NULL,
    supplier_name   VARCHAR(200)  NOT NULL DEFAULT '',

    payment_no      VARCHAR(50)   NOT NULL,
    -- ADVANCE is a deposit: at the moment it leaves, no receipt and no
    -- invoice exist. It cannot settle anything yet — it parks against the
    -- order and is re-pointed at the invoice when one arrives.
    payment_type    VARCHAR(16)   NOT NULL DEFAULT 'SETTLEMENT'
        CHECK (payment_type IN ('ADVANCE','SETTLEMENT','REFUND')),

    currency        VARCHAR(8)    NOT NULL,
    amount          NUMERIC(18,2) NOT NULL CHECK (amount > 0),
    -- Base-currency view and the rate used. Zero until P6 wires fx; kept
    -- from day one because a USD order paid in CNY leaves a gap that must
    -- land in the ledger, not be swallowed.
    base_currency   VARCHAR(8)    NOT NULL DEFAULT 'CNY',
    base_amount     NUMERIC(18,2) NOT NULL DEFAULT 0,
    fx_rate         NUMERIC(18,8) NOT NULL DEFAULT 0,

    paid_at         DATE          NOT NULL,
    method          VARCHAR(24)   NOT NULL DEFAULT 'WIRE'
        CHECK (method IN ('WIRE','LC','TT','CASH','OTHER')),

    -- The bank's own reference, once statements arrive. See header note.
    bank_ref        VARCHAR(100)  NOT NULL DEFAULT '',
    bank_txn_id     BIGINT,

    remark          TEXT          NOT NULL DEFAULT '',
    created_by_id   BIGINT        NOT NULL DEFAULT 0,
    created_by_name VARCHAR(100)  NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),

    UNIQUE (tenant_id, payment_no)
);
CREATE INDEX supplier_payments_supplier_idx
    ON supplier_payments (tenant_id, supplier_id, paid_at DESC);

-- Where each payment went. The core of the reconciliation, shaped after
-- export's receipt_allocations because that shape has already survived
-- contact with reality:
--   - append-only: a reversal is a negative row pointing at what it undoes,
--     never an UPDATE or DELETE. Summing gives today's answer; reading in
--     order gives how somebody got there, including the mistake.
--   - fee_amount is money spent that settles nothing (intermediary bank
--     charges), kept apart so it can become a ledger expense later.
--
-- One deliberate difference: the target is EITHER an invoice (settlement)
-- OR a purchase order (deposit) — exactly one. A deposit leaves before any
-- invoice exists, so "allocate to invoice" alone cannot represent the most
-- common payment in this trade.
CREATE TABLE payment_allocations (
    id             BIGSERIAL     PRIMARY KEY,
    tenant_id      BIGINT        NOT NULL DEFAULT 1,
    payment_id     BIGINT        NOT NULL REFERENCES supplier_payments(id),

    invoice_id     BIGINT        REFERENCES supplier_invoices(id),
    po_id          BIGINT        REFERENCES purchase_orders(id),
    CHECK ((invoice_id IS NOT NULL) <> (po_id IS NOT NULL)),

    amount         NUMERIC(18,2) NOT NULL CHECK (amount <> 0),
    fee_amount     NUMERIC(18,2) NOT NULL DEFAULT 0,
    currency       VARCHAR(8)    NOT NULL,

    reversal_of    BIGINT        REFERENCES payment_allocations(id),
    reverse_reason TEXT          NOT NULL DEFAULT '',

    allocated_by      BIGINT       NOT NULL DEFAULT 0,
    allocated_by_name VARCHAR(100) NOT NULL DEFAULT '',
    allocated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX payment_alloc_payment_idx ON payment_allocations (tenant_id, payment_id);
CREATE INDEX payment_alloc_invoice_idx ON payment_allocations (tenant_id, invoice_id)
    WHERE invoice_id IS NOT NULL;
CREATE INDEX payment_alloc_po_idx ON payment_allocations (tenant_id, po_id)
    WHERE po_id IS NOT NULL;
-- One reversal per row. Without this, two clicks on 冲销 undo the same
-- allocation twice and the payment's free balance doubles what it should.
CREATE UNIQUE INDEX payment_alloc_reversal_once
    ON payment_allocations (reversal_of) WHERE reversal_of IS NOT NULL;

-- +goose Down
DROP TABLE payment_allocations;
DROP TABLE supplier_payments;

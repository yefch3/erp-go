-- +goose Up

-- The third leg of the three-way match.
--
-- A purchase order is our promise; a receipt is what we saw arrive; this is
-- the factory's claim of what we owe. The first two are our own records — the
-- invoice is the only one of the three written by the other side, which is
-- why it gets stored verbatim and *checked against* the other two rather
-- than trusted. Matching (P2) reads all three; nothing here is derived.
CREATE TABLE supplier_invoices (
    id              BIGSERIAL     PRIMARY KEY,
    tenant_id       BIGINT        NOT NULL DEFAULT 1,

    -- Supplier snapshot, same reasoning as purchase_orders: renaming the
    -- supplier in masterdata must not rewrite what this paper said.
    supplier_id     BIGINT        NOT NULL,
    supplier_code   VARCHAR(50)   NOT NULL DEFAULT '',
    supplier_name   VARCHAR(200)  NOT NULL DEFAULT '',

    -- The factory's own number. Unique per supplier: entering the same paper
    -- twice is the most common way a supplier gets paid twice.
    invoice_no      VARCHAR(100)  NOT NULL,

    -- What kind of paper this is. It decides whether the document can back a
    -- VAT export rebate later, so it is captured at entry while somebody is
    -- holding the original, not reconstructed afterwards.
    invoice_type    VARCHAR(24)   NOT NULL DEFAULT 'COMMERCIAL'
        CHECK (invoice_type IN ('COMMERCIAL','PROFORMA','VAT_SPECIAL','VAT_PLAIN','OTHER')),

    currency        VARCHAR(8)    NOT NULL,
    -- What the factory says we owe. Their claim, not our conclusion.
    total_amount    NUMERIC(18,2) NOT NULL CHECK (total_amount > 0),
    tax_amount      NUMERIC(18,2) NOT NULL DEFAULT 0 CHECK (tax_amount >= 0),

    invoice_date    DATE          NOT NULL,
    -- Payment-term deadline. The overdue list keys on it; nullable because
    -- proformas and deposit requests often carry none.
    due_date        DATE,

    -- Three-way match verdict, written by the matcher (P2). PENDING until it
    -- runs. EXCEPTION marks a discrepancy without blocking anything — the
    -- factory adding 2% for freight is an everyday event the buyer decides
    -- on, not an error the system should refuse to record.
    match_status    VARCHAR(16)   NOT NULL DEFAULT 'PENDING'
        CHECK (match_status IN ('PENDING','MATCHED','EXCEPTION')),
    match_note      TEXT          NOT NULL DEFAULT '',

    status          VARCHAR(16)   NOT NULL DEFAULT 'OPEN'
        CHECK (status IN ('OPEN','SETTLED','VOID')),
    void_reason     TEXT          NOT NULL DEFAULT '',

    -- Scan of the paper. The column exists from day one so nothing needs a
    -- second migration; the upload wiring lands when procurement grows a
    -- file-storage adapter (it has none today).
    attachment_key  VARCHAR(500)  NOT NULL DEFAULT '',

    created_by_id   BIGINT        NOT NULL DEFAULT 0,
    created_by_name VARCHAR(100)  NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),

    UNIQUE (tenant_id, supplier_id, invoice_no)
);
CREATE INDEX supplier_invoices_supplier_idx
    ON supplier_invoices (tenant_id, supplier_id, status, invoice_date DESC);
CREATE INDEX supplier_invoices_status_idx
    ON supplier_invoices (tenant_id, status, created_at DESC);
-- The overdue list: open invoices ordered by deadline.
CREATE INDEX supplier_invoices_due_idx
    ON supplier_invoices (tenant_id, due_date)
    WHERE status = 'OPEN' AND due_date IS NOT NULL;

-- One claimed line. po_item_id nullable on purpose: factories bill charges
-- that exist on no order — freight, sample fees, mould costs. Those lines
-- can never MATCH, but refusing to record them would just push them into a
-- spreadsheet where nobody reconciles them at all.
CREATE TABLE supplier_invoice_lines (
    id           BIGSERIAL     PRIMARY KEY,
    tenant_id    BIGINT        NOT NULL DEFAULT 1,
    invoice_id   BIGINT        NOT NULL REFERENCES supplier_invoices(id) ON DELETE CASCADE,
    po_id        BIGINT        REFERENCES purchase_orders(id),
    po_item_id   BIGINT        REFERENCES purchase_order_items(id),
    description  VARCHAR(300)  NOT NULL DEFAULT '',
    qty          NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (qty >= 0),
    unit_price   NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (unit_price >= 0),
    amount       NUMERIC(18,2) NOT NULL
);
CREATE INDEX supplier_invoice_lines_invoice_idx ON supplier_invoice_lines (invoice_id);
CREATE INDEX supplier_invoice_lines_po_idx
    ON supplier_invoice_lines (tenant_id, po_id) WHERE po_id IS NOT NULL;

-- +goose Down
DROP TABLE supplier_invoice_lines;
DROP TABLE supplier_invoices;

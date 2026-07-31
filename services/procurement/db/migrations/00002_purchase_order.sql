-- +goose Up

-- Requirements gain the other half of their life cycle. ordered_qty was
-- already here; without received_qty a requirement can say "somebody promised
-- to send this" but never "it turned up", and a manual requirement — one with
-- no contract behind it — would have no way of ever closing.
ALTER TABLE purchase_requirements
    ADD COLUMN received_qty NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (received_qty >= 0);

ALTER TABLE purchase_requirements DROP CONSTRAINT purchase_requirements_status_check;
ALTER TABLE purchase_requirements ADD CONSTRAINT purchase_requirements_status_check
    CHECK (status IN ('PENDING', 'PARTIALLY_ORDERED', 'ORDERED', 'RECEIVED',
                      'SUPERSEDED', 'CANCELLED'));

-- A purchase order: one supplier, one approval, many requirement lines.
--
-- Consolidation across contracts is the whole point of the document. Three
-- contracts each short 500 become one order for 1500, which is the only way a
-- buyer gets a better price — so an order references requirements rather than
-- contracts, and a requirement may be spread over several orders.
CREATE TABLE purchase_orders (
    id              BIGSERIAL     PRIMARY KEY,
    tenant_id       BIGINT        NOT NULL DEFAULT 1,
    po_no           VARCHAR(50)   NOT NULL,

    -- Supplier snapshot. masterdata owns the supplier; this is what the order
    -- was placed with, and renaming the supplier later must not rewrite it.
    supplier_id     BIGINT        NOT NULL,
    supplier_code   VARCHAR(50)   NOT NULL DEFAULT '',
    supplier_name   VARCHAR(200)  NOT NULL DEFAULT '',

    currency        VARCHAR(8)    NOT NULL DEFAULT 'CNY',
    total_amount    NUMERIC(18,2) NOT NULL DEFAULT 0 CHECK (total_amount >= 0),
    expected_date   DATE,

    status          VARCHAR(32)   NOT NULL DEFAULT 'DRAFT'
                    CHECK (status IN ('DRAFT', 'PENDING_APPROVAL', 'REJECTED',
                                      'ORDERED', 'PARTIALLY_RECEIVED', 'RECEIVED',
                                      'CANCELLED')),
    -- The approval engine's handle. Nullable: a draft has not been submitted.
    approval_instance_id BIGINT,
    reject_reason   TEXT          NOT NULL DEFAULT '',
    cancel_reason   TEXT          NOT NULL DEFAULT '',

    buyer_id        BIGINT        NOT NULL DEFAULT 0,
    buyer_name      VARCHAR(100)  NOT NULL DEFAULT '',
    remark          TEXT          NOT NULL DEFAULT '',

    ordered_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, po_no)
);
CREATE INDEX purchase_orders_status_idx ON purchase_orders (tenant_id, status, created_at DESC);
CREATE INDEX purchase_orders_supplier_idx ON purchase_orders (tenant_id, supplier_id);

CREATE TABLE purchase_order_items (
    id             BIGSERIAL     PRIMARY KEY,
    tenant_id      BIGINT        NOT NULL DEFAULT 1,
    po_id          BIGINT        NOT NULL REFERENCES purchase_orders(id) ON DELETE CASCADE,
    requirement_id BIGINT        NOT NULL REFERENCES purchase_requirements(id),

    -- Snapshot again, for the same reason as everywhere else in this system.
    product_id     BIGINT        NOT NULL,
    sku_id         BIGINT,
    product_code   VARCHAR(100)  NOT NULL DEFAULT '',
    product_name   VARCHAR(200)  NOT NULL DEFAULT '',
    spec           VARCHAR(300)  NOT NULL DEFAULT '',
    uom_id         BIGINT        NOT NULL DEFAULT 0,
    uom_code       VARCHAR(32)   NOT NULL DEFAULT '',

    qty            NUMERIC(18,4) NOT NULL CHECK (qty > 0),
    unit_price     NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (unit_price >= 0),
    amount         NUMERIC(18,2) NOT NULL DEFAULT 0 CHECK (amount >= 0),
    received_qty   NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (received_qty >= 0),

    -- One line per requirement per order. Ordering the same requirement twice
    -- on one document is a mistake, not a partial delivery.
    UNIQUE (po_id, requirement_id),
    CHECK (received_qty <= qty)
);
CREATE INDEX purchase_order_items_po_idx ON purchase_order_items (po_id);
CREATE INDEX purchase_order_items_req_idx ON purchase_order_items (tenant_id, requirement_id);

-- Goods arriving. Separate from the order because deliveries are partial far
-- more often than not, and "how much came, and when" is a question the order
-- alone cannot answer.
CREATE TABLE purchase_receipts (
    id            BIGSERIAL     PRIMARY KEY,
    tenant_id     BIGINT        NOT NULL DEFAULT 1,
    po_id         BIGINT        NOT NULL REFERENCES purchase_orders(id),
    receipt_no    VARCHAR(50)   NOT NULL,
    warehouse_id  BIGINT        NOT NULL DEFAULT 0,
    operator_id   BIGINT        NOT NULL DEFAULT 0,
    operator_name VARCHAR(100)  NOT NULL DEFAULT '',
    remark        TEXT          NOT NULL DEFAULT '',
    received_at   TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, receipt_no)
);
CREATE INDEX purchase_receipts_po_idx ON purchase_receipts (po_id);

CREATE TABLE purchase_receipt_items (
    id          BIGSERIAL     PRIMARY KEY,
    tenant_id   BIGINT        NOT NULL DEFAULT 1,
    receipt_id  BIGINT        NOT NULL REFERENCES purchase_receipts(id) ON DELETE CASCADE,
    po_item_id  BIGINT        NOT NULL REFERENCES purchase_order_items(id),
    qty         NUMERIC(18,4) NOT NULL CHECK (qty > 0)
);
CREATE INDEX purchase_receipt_items_receipt_idx ON purchase_receipt_items (receipt_id);

-- Procurement was a pure consumer until now. Receiving goods is the first
-- thing it has to tell the rest of the system about, so it needs an outbox of
-- its own: the stock increase and the receipt record must either both happen
-- or neither, and a direct call to inventory cannot promise that.
CREATE TABLE outbox_events (
    id             BIGSERIAL    PRIMARY KEY,
    tenant_id      BIGINT       NOT NULL DEFAULT 1,
    aggregate_type VARCHAR(50)  NOT NULL,
    aggregate_id   VARCHAR(100) NOT NULL,
    event_type     VARCHAR(100) NOT NULL,
    payload        JSONB        NOT NULL,
    trace_id       VARCHAR(64)  NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    published_at   TIMESTAMPTZ,
    attempts       INT          NOT NULL DEFAULT 0,
    last_error     TEXT
);
CREATE INDEX outbox_events_unpublished_idx
    ON outbox_events (created_at) WHERE published_at IS NULL;

-- +goose Down
DROP TABLE outbox_events;
DROP TABLE purchase_receipt_items;
DROP TABLE purchase_receipts;
DROP TABLE purchase_order_items;
DROP TABLE purchase_orders;
ALTER TABLE purchase_requirements DROP CONSTRAINT purchase_requirements_status_check;
ALTER TABLE purchase_requirements ADD CONSTRAINT purchase_requirements_status_check
    CHECK (status IN ('PENDING', 'PARTIALLY_ORDERED', 'ORDERED', 'SUPERSEDED', 'CANCELLED'));
ALTER TABLE purchase_requirements DROP COLUMN received_qty;

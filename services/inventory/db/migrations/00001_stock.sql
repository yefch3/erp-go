-- +goose Up

CREATE TABLE warehouses (
    id         BIGSERIAL    PRIMARY KEY,
    tenant_id  BIGINT       NOT NULL DEFAULT 1,
    code       VARCHAR(50)  NOT NULL,
    name       VARCHAR(100) NOT NULL,
    wh_type    VARCHAR(32)  NOT NULL DEFAULT 'NORMAL'
               CHECK (wh_type IN ('NORMAL', 'BONDED', 'TRANSIT', 'VIRTUAL')),
    address    TEXT         NOT NULL DEFAULT '',
    manager_id BIGINT       NOT NULL DEFAULT 0,
    status     VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE'
               CHECK (status IN ('ACTIVE', 'INACTIVE')),
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, code)
);

-- Stock, one row per warehouse and SKU.
--
-- Four ways a quantity can be spoken for, and they are NOT the same thing:
--
--   on_hand   physically in the warehouse
--   reserved  sold on an effective contract but not yet picked   ← layer 1
--   locked    committed to a confirmed shipment plan             ← layer 2
--   frozen    quarantined, failed inspection, being counted
--
-- available = on_hand - reserved - locked - frozen, as a generated column so
-- nothing can compute it differently somewhere else.
--
-- The two allocation layers exist because export lead times are long. A
-- contract signed in July shipping in October has to take its goods out of
-- circulation on the day it is signed, or the same crate gets sold twice; but
-- it must not be pinned to a picking location for three months either.
-- Confirming a shipment plan converts a reservation into a lock, which moves
-- quantity between the two columns and leaves available untouched.
CREATE TABLE stocks (
    id             BIGSERIAL     PRIMARY KEY,
    tenant_id      BIGINT        NOT NULL DEFAULT 1,
    warehouse_id   BIGINT        NOT NULL REFERENCES warehouses(id),
    product_id     BIGINT        NOT NULL,
    sku_id         BIGINT        NOT NULL,
    uom_id         BIGINT        NOT NULL DEFAULT 0,
    uom_code       VARCHAR(32)   NOT NULL DEFAULT '',
    product_code   VARCHAR(100)  NOT NULL DEFAULT '',
    product_name   VARCHAR(200)  NOT NULL DEFAULT '',
    on_hand_qty    NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (on_hand_qty >= 0),
    reserved_qty   NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (reserved_qty >= 0),
    locked_qty     NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (locked_qty >= 0),
    frozen_qty     NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (frozen_qty >= 0),
    in_transit_qty NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (in_transit_qty >= 0),
    available_qty  NUMERIC(18,4)
                   GENERATED ALWAYS AS (on_hand_qty - reserved_qty - locked_qty - frozen_qty) STORED,
    updated_at     TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, warehouse_id, sku_id),
    -- You cannot promise more than you physically hold. This is the invariant
    -- that makes overselling impossible rather than merely unlikely.
    CHECK (reserved_qty + locked_qty + frozen_qty <= on_hand_qty)
);
CREATE INDEX stocks_sku_idx ON stocks (tenant_id, sku_id);

-- Layer 1. One row per contract line, which is also the idempotency key: a
-- redelivered ContractEffective conflicts instead of reserving twice.
--
-- demand_qty and reserved_qty are separate on purpose. When stock cannot
-- cover the contract, the difference is the shortage that becomes a purchase
-- requirement — and keeping the demand means a later top-up, once goods
-- arrive, knows what it is topping up towards.
CREATE TABLE stock_reservations (
    id           BIGSERIAL     PRIMARY KEY,
    tenant_id    BIGINT        NOT NULL DEFAULT 1,
    ref_type     VARCHAR(32)   NOT NULL CHECK (ref_type IN ('CONTRACT')),
    ref_id       BIGINT        NOT NULL,
    ref_line_id  BIGINT        NOT NULL,
    ref_no       VARCHAR(50)   NOT NULL DEFAULT '',
    product_id   BIGINT        NOT NULL,
    sku_id       BIGINT        NOT NULL,
    demand_qty   NUMERIC(18,4) NOT NULL CHECK (demand_qty > 0),
    reserved_qty NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (reserved_qty >= 0),
    shortage_qty NUMERIC(18,4) GENERATED ALWAYS AS (demand_qty - reserved_qty) STORED,
    status       VARCHAR(32)   NOT NULL DEFAULT 'ACTIVE'
                 CHECK (status IN ('ACTIVE', 'CONVERTED', 'RELEASED')),
    created_at   TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CHECK (reserved_qty <= demand_qty),
    UNIQUE (tenant_id, ref_type, ref_id, ref_line_id)
);
CREATE INDEX stock_reservations_ref_idx ON stock_reservations (tenant_id, ref_type, ref_id);

-- Which warehouse rows a reservation actually took its quantity from. Without
-- this, releasing a reservation could not put the quantity back where it came
-- from when stock is spread over several warehouses.
CREATE TABLE stock_reservation_lines (
    id             BIGSERIAL     PRIMARY KEY,
    tenant_id      BIGINT        NOT NULL DEFAULT 1,
    reservation_id BIGINT        NOT NULL REFERENCES stock_reservations(id) ON DELETE CASCADE,
    stock_id       BIGINT        NOT NULL REFERENCES stocks(id),
    warehouse_id   BIGINT        NOT NULL,
    qty            NUMERIC(18,4) NOT NULL CHECK (qty > 0)
);
CREATE INDEX stock_reservation_lines_res_idx ON stock_reservation_lines (reservation_id);

-- Layer 2. Idempotent by the reference that caused it, same as reservations.
CREATE TABLE stock_locks (
    id            BIGSERIAL     PRIMARY KEY,
    tenant_id     BIGINT        NOT NULL DEFAULT 1,
    lock_ref_type VARCHAR(50)   NOT NULL,
    lock_ref_id   BIGINT        NOT NULL,
    warehouse_id  BIGINT        NOT NULL,
    sku_id        BIGINT        NOT NULL,
    qty           NUMERIC(18,4) NOT NULL CHECK (qty > 0),
    status        VARCHAR(32)   NOT NULL DEFAULT 'ACTIVE'
                  CHECK (status IN ('ACTIVE', 'CONSUMED', 'RELEASED')),
    locked_at     TIMESTAMPTZ   NOT NULL DEFAULT now(),
    released_at   TIMESTAMPTZ,
    UNIQUE (tenant_id, lock_ref_type, lock_ref_id, sku_id)
);

-- Every movement, append-only. The stock table says what is there now; this
-- says how it got there, and the two must always reconcile.
CREATE TABLE stock_ledger (
    id           BIGSERIAL     PRIMARY KEY,
    tenant_id    BIGINT        NOT NULL DEFAULT 1,
    stock_id     BIGINT        NOT NULL REFERENCES stocks(id),
    warehouse_id BIGINT        NOT NULL,
    sku_id       BIGINT        NOT NULL,
    -- INBOUND / OUTBOUND / RESERVE / RELEASE_RESERVE / LOCK / RELEASE_LOCK /
    -- CONVERT_RESERVE_TO_LOCK / FREEZE / UNFREEZE / ADJUST
    movement     VARCHAR(32)   NOT NULL,
    qty          NUMERIC(18,4) NOT NULL,
    ref_type     VARCHAR(50)   NOT NULL DEFAULT '',
    ref_id       BIGINT        NOT NULL DEFAULT 0,
    ref_no       VARCHAR(50)   NOT NULL DEFAULT '',
    on_hand_after   NUMERIC(18,4) NOT NULL,
    available_after NUMERIC(18,4) NOT NULL,
    operator_id  BIGINT        NOT NULL DEFAULT 0,
    remark       TEXT          NOT NULL DEFAULT '',
    occurred_at  TIMESTAMPTZ   NOT NULL DEFAULT now()
);
CREATE INDEX stock_ledger_stock_idx ON stock_ledger (tenant_id, stock_id, occurred_at DESC);

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

CREATE TABLE processed_events (
    event_id       VARCHAR(200) NOT NULL,
    consumer_group VARCHAR(100) NOT NULL,
    processed_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, consumer_group)
);

-- A default warehouse, so a fresh install has somewhere to put stock. Without
-- one, every inbound would fail on a system nobody has configured yet.
INSERT INTO warehouses (tenant_id, code, name, wh_type) VALUES (1, 'WH01', '主仓库', 'NORMAL');

-- +goose Down
DROP TABLE processed_events;
DROP TABLE outbox_events;
DROP TABLE stock_ledger;
DROP TABLE stock_locks;
DROP TABLE stock_reservation_lines;
DROP TABLE stock_reservations;
DROP TABLE stocks;
DROP TABLE warehouses;

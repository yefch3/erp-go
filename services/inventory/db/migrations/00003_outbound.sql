-- +goose Up

-- A reservation now has to survive longer than the moment it was made, so it
-- carries the contract snapshot with it.
--
-- The reason is the top-up. A contract line that fell short raised a purchase
-- requirement; when those goods arrive, the reservation takes the missing
-- quantity and the requirement has to be told the shortage shrank. Telling it
-- means re-publishing StockAllocated, which needs the customer, the delivery
-- date and the product description — none of which are worth a synchronous
-- call back into export at the moment somebody is standing at a shelf.
ALTER TABLE stock_reservations
    ADD COLUMN version_id     BIGINT       NOT NULL DEFAULT 0,
    ADD COLUMN version_no     INT          NOT NULL DEFAULT 0,
    ADD COLUMN customer_name  VARCHAR(200) NOT NULL DEFAULT '',
    -- Text, not DATE: it is a snapshot of what the event said, and it goes
    -- back out on an event unchanged.
    ADD COLUMN delivery_date  VARCHAR(20)  NOT NULL DEFAULT '',
    ADD COLUMN product_code   VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN product_name   VARCHAR(200) NOT NULL DEFAULT '',
    ADD COLUMN uom_id         BIGINT       NOT NULL DEFAULT 0,
    ADD COLUMN uom_code       VARCHAR(32)  NOT NULL DEFAULT '',
    -- Of what this line secured, how much has moved on. reserved_qty itself
    -- never falls when quantity is locked or shipped: it means "covered by
    -- stock we hold", and shortage_qty is derived from it. Decrementing it
    -- would make a fully shipped line look short again and have somebody buy
    -- goods that already left the building.
    ADD COLUMN locked_qty     NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (locked_qty >= 0),
    ADD COLUMN shipped_qty    NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (shipped_qty >= 0),
    ADD CONSTRAINT stock_reservations_committed_chk
        CHECK (locked_qty + shipped_qty <= reserved_qty);

-- SHIPPED closes the line: everything it was owed has physically left.
ALTER TABLE stock_reservations DROP CONSTRAINT stock_reservations_status_check;
ALTER TABLE stock_reservations ADD CONSTRAINT stock_reservations_status_check
    CHECK (status IN ('ACTIVE', 'CONVERTED', 'RELEASED', 'SHIPPED'));

-- How much of this particular warehouse row's contribution is already spoken
-- for by an open or completed outbound. Needed because a reservation spread
-- over three warehouses must not hand the same crate to two picking lists.
ALTER TABLE stock_reservation_lines
    ADD COLUMN committed_qty NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (committed_qty >= 0),
    ADD CONSTRAINT stock_reservation_lines_committed_chk CHECK (committed_qty <= qty);

-- An outbound is a picking list that became a shipment.
--
-- There is no warehouse on the header on purpose: which warehouses the goods
-- come out of was decided when the contract reserved them, and pretending the
-- picker chooses would be a second, conflicting answer.
CREATE TABLE outbounds (
    id               BIGSERIAL    PRIMARY KEY,
    tenant_id        BIGINT       NOT NULL DEFAULT 1,
    outbound_no      VARCHAR(50)  NOT NULL,
    outbound_type    VARCHAR(32)  NOT NULL DEFAULT 'SALES'
                     CHECK (outbound_type IN ('SALES', 'SAMPLE', 'TRANSFER', 'OTHER')),
    -- Today only a contract can pull goods out. Shipment plans will slot in
    -- here later without changing anything below.
    ref_type         VARCHAR(32)  NOT NULL DEFAULT 'CONTRACT'
                     CHECK (ref_type IN ('CONTRACT')),
    ref_id           BIGINT       NOT NULL,
    ref_no           VARCHAR(50)  NOT NULL DEFAULT '',
    customer_name    VARCHAR(200) NOT NULL DEFAULT '',
    status           VARCHAR(32)  NOT NULL DEFAULT 'DRAFT'
                     CHECK (status IN ('DRAFT', 'CONFIRMED', 'CANCELLED')),
    operator_id      BIGINT       NOT NULL DEFAULT 0,
    operator_name    VARCHAR(100) NOT NULL DEFAULT '',
    remark           TEXT         NOT NULL DEFAULT '',
    cancelled_reason TEXT         NOT NULL DEFAULT '',
    confirmed_at     TIMESTAMPTZ,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, outbound_no)
);
CREATE INDEX outbounds_ref_idx ON outbounds (tenant_id, ref_type, ref_id);
CREATE INDEX outbounds_status_idx ON outbounds (tenant_id, status, created_at DESC);

CREATE TABLE outbound_items (
    id             BIGSERIAL     PRIMARY KEY,
    tenant_id      BIGINT        NOT NULL DEFAULT 1,
    outbound_id    BIGINT        NOT NULL REFERENCES outbounds(id) ON DELETE CASCADE,
    reservation_id BIGINT        NOT NULL REFERENCES stock_reservations(id),
    ref_line_id    BIGINT        NOT NULL,
    product_id     BIGINT        NOT NULL,
    sku_id         BIGINT        NOT NULL,
    product_code   VARCHAR(100)  NOT NULL DEFAULT '',
    product_name   VARCHAR(200)  NOT NULL DEFAULT '',
    uom_code       VARCHAR(32)   NOT NULL DEFAULT '',
    qty            NUMERIC(18,4) NOT NULL CHECK (qty > 0),
    -- One contract line appears at most once per picking list; two partial
    -- shipments of the same line are two outbounds, which is also how the
    -- warehouse counts them.
    UNIQUE (outbound_id, ref_line_id)
);
CREATE INDEX outbound_items_outbound_idx ON outbound_items (outbound_id);

-- Which warehouse rows each picked line draws from. Confirming decrements
-- exactly these; cancelling puts back exactly these.
CREATE TABLE outbound_item_stocks (
    id                  BIGSERIAL     PRIMARY KEY,
    tenant_id           BIGINT        NOT NULL DEFAULT 1,
    outbound_item_id    BIGINT        NOT NULL REFERENCES outbound_items(id) ON DELETE CASCADE,
    reservation_line_id BIGINT        NOT NULL REFERENCES stock_reservation_lines(id),
    stock_id            BIGINT        NOT NULL REFERENCES stocks(id),
    warehouse_id        BIGINT        NOT NULL,
    qty                 NUMERIC(18,4) NOT NULL CHECK (qty > 0)
);
CREATE INDEX outbound_item_stocks_item_idx ON outbound_item_stocks (outbound_item_id);

-- +goose Down
DROP TABLE outbound_item_stocks;
DROP TABLE outbound_items;
DROP TABLE outbounds;
ALTER TABLE stock_reservation_lines
    DROP CONSTRAINT stock_reservation_lines_committed_chk,
    DROP COLUMN committed_qty;
ALTER TABLE stock_reservations DROP CONSTRAINT stock_reservations_status_check;
ALTER TABLE stock_reservations ADD CONSTRAINT stock_reservations_status_check
    CHECK (status IN ('ACTIVE', 'CONVERTED', 'RELEASED'));
ALTER TABLE stock_reservations
    DROP CONSTRAINT stock_reservations_committed_chk,
    DROP COLUMN shipped_qty,
    DROP COLUMN locked_qty,
    DROP COLUMN uom_code,
    DROP COLUMN uom_id,
    DROP COLUMN product_name,
    DROP COLUMN product_code,
    DROP COLUMN delivery_date,
    DROP COLUMN customer_name,
    DROP COLUMN version_no,
    DROP COLUMN version_id;

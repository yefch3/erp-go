-- +goose Up

-- The shipping document: what went on which boat.
--
-- Goods go straight from the supplier to the vessel — there is no warehouse in
-- the middle — so this is entered by hand rather than derived from an outbound.
-- It is the primary record of what physically left, and the inventory path
-- (§5.6.12) remains as a second source for the contracts that do pass through
-- stock. Both land in contract_shipments, which is the ledger both read.
CREATE TABLE shipments (
    id                BIGSERIAL     PRIMARY KEY,
    tenant_id         BIGINT        NOT NULL DEFAULT 1,
    shipment_no       VARCHAR(50)   NOT NULL,
    vessel_name       VARCHAR(200)  NOT NULL DEFAULT '',
    voyage_no         VARCHAR(50)   NOT NULL DEFAULT '',
    bl_no             VARCHAR(100)  NOT NULL DEFAULT '',
    -- Free text, not one code: a single booking often covers several boxes and
    -- the forwarder sends them as one line. Splitting them into rows would be
    -- inventing structure the paperwork does not have.
    container_no      VARCHAR(500)  NOT NULL DEFAULT '',
    port_of_discharge VARCHAR(200)  NOT NULL DEFAULT '',
    etd               DATE,
    eta               DATE,
    status            VARCHAR(20)   NOT NULL DEFAULT 'DRAFT'
        CHECK (status IN ('DRAFT','SHIPPED','ARRIVED','CANCELLED')),
    remark            TEXT          NOT NULL DEFAULT '',
    created_by        BIGINT        NOT NULL,
    -- Snapshot: who booked it, readable a year later without asking iam.
    created_by_name   VARCHAR(100)  NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ   NOT NULL DEFAULT now(),
    shipped_at        TIMESTAMPTZ,
    arrived_at        TIMESTAMPTZ,
    UNIQUE (tenant_id, shipment_no)
);
CREATE INDEX shipments_status_idx ON shipments (tenant_id, status, etd DESC);
CREATE INDEX shipments_bl_idx     ON shipments (tenant_id, bl_no) WHERE bl_no <> '';

-- One line of the packing list. The contract sits here rather than on the
-- header, and that is the whole reason this is two tables: one container is
-- routinely shared between several contracts for the same customer, or several
-- customers on the same sailing. A contract_id on the header would force a
-- consolidated box to be split into fictional shipments that never existed.
CREATE TABLE shipment_items (
    id               BIGSERIAL     PRIMARY KEY,
    tenant_id        BIGINT        NOT NULL DEFAULT 1,
    shipment_id      BIGINT        NOT NULL REFERENCES shipments(id) ON DELETE CASCADE,
    line_no          INT           NOT NULL,
    contract_id      BIGINT        NOT NULL REFERENCES contracts(id),
    contract_no      VARCHAR(50)   NOT NULL,
    customer_name    VARCHAR(200)  NOT NULL DEFAULT '',
    -- References contract_items(id) without a foreign key, same reasoning as
    -- contract_shipments: the line may belong to a version since superseded,
    -- and the goods still went on the boat.
    contract_item_id BIGINT        NOT NULL,
    product_id       BIGINT        NOT NULL,
    sku_id           BIGINT        NOT NULL DEFAULT 0,
    product_code     VARCHAR(50)   NOT NULL DEFAULT '',
    product_name     VARCHAR(200)  NOT NULL,
    spec             VARCHAR(200)  NOT NULL DEFAULT '',
    qty              NUMERIC(18,4) NOT NULL CHECK (qty > 0),
    uom_code         VARCHAR(20)   NOT NULL DEFAULT '',
    -- One row per contract line per shipment. Two boxes of the same goods on
    -- the same sailing are one line with the total, because that is what
    -- contract_shipments can hold — its uniqueness is (outbound_no, item).
    UNIQUE (tenant_id, shipment_id, contract_item_id)
);
CREATE INDEX shipment_items_shipment_idx ON shipment_items (tenant_id, shipment_id, line_no);
CREATE INDEX shipment_items_contract_idx ON shipment_items (tenant_id, contract_id);

-- +goose Down
DROP TABLE shipment_items;
DROP TABLE shipments;

-- +goose Up
CREATE TABLE quotations (
    id                BIGSERIAL     PRIMARY KEY,
    tenant_id         BIGINT        NOT NULL DEFAULT 1,
    quote_no          VARCHAR(50)   NOT NULL,
    customer_id       BIGINT        NOT NULL,
    -- Denormalized so a quotation still reads correctly if the customer is
    -- renamed later; the id remains the reference.
    customer_name     VARCHAR(200)  NOT NULL DEFAULT '',
    currency          CHAR(3)       NOT NULL,
    incoterm          VARCHAR(20)   NOT NULL DEFAULT 'FOB',
    port_of_loading   VARCHAR(100)  NOT NULL DEFAULT '',
    port_of_discharge VARCHAR(100)  NOT NULL DEFAULT '',
    payment_method    VARCHAR(50)   NOT NULL DEFAULT '',
    valid_until       DATE,
    -- Exchange-rate snapshot, embedded as a value object. There is no
    -- foreign key to the fx tables on purpose: today's rate moving must not
    -- change what this quotation said when it was sent.
    fx_rate           NUMERIC(18,8) NOT NULL,
    fx_rate_at        TIMESTAMPTZ   NOT NULL,
    fx_source         VARCHAR(50)   NOT NULL,
    fx_base_currency  CHAR(3)       NOT NULL DEFAULT 'USD',
    total_amount      NUMERIC(18,2) NOT NULL DEFAULT 0,
    -- The same total converted at the snapshot rate, so reports can sum
    -- across currencies without re-deriving anything.
    base_amount       NUMERIC(18,2) NOT NULL DEFAULT 0,
    remark            TEXT          NOT NULL DEFAULT '',
    status            VARCHAR(32)   NOT NULL DEFAULT 'DRAFT'
                      CHECK (status IN ('DRAFT','SENT','ACCEPTED','REJECTED','EXPIRED','CANCELLED')),
    sales_employee_id BIGINT        NOT NULL DEFAULT 0,
    sales_employee    VARCHAR(100)  NOT NULL DEFAULT '',
    sent_at           TIMESTAMPTZ,
    responded_at      TIMESTAMPTZ,
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT now(),
    created_by        BIGINT        NOT NULL DEFAULT 0,
    updated_at        TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_by        BIGINT        NOT NULL DEFAULT 0,
    UNIQUE (tenant_id, quote_no)
);
CREATE INDEX quotations_customer_idx ON quotations (tenant_id, customer_id, created_at DESC);
CREATE INDEX quotations_status_idx ON quotations (tenant_id, status);

CREATE TABLE quotation_items (
    id           BIGSERIAL     PRIMARY KEY,
    tenant_id    BIGINT        NOT NULL DEFAULT 1,
    quotation_id BIGINT        NOT NULL REFERENCES quotations(id) ON DELETE CASCADE,
    line_no      INT           NOT NULL,
    product_id   BIGINT        NOT NULL,
    sku_id       BIGINT,
    -- Product fields are copied at quoting time: renaming a product must not
    -- rewrite what was quoted to a customer last month.
    product_code VARCHAR(100)  NOT NULL,
    product_name VARCHAR(200)  NOT NULL,
    spec         VARCHAR(500)  NOT NULL DEFAULT '',
    qty          NUMERIC(18,4) NOT NULL CHECK (qty > 0),
    uom_id       BIGINT        NOT NULL DEFAULT 0,
    uom_code     VARCHAR(20)   NOT NULL DEFAULT '',
    unit_price   NUMERIC(18,4) NOT NULL CHECK (unit_price >= 0),
    amount       NUMERIC(18,2) NOT NULL,
    remark       TEXT          NOT NULL DEFAULT '',
    UNIQUE (quotation_id, line_no)
);
CREATE INDEX quotation_items_quotation_idx ON quotation_items (tenant_id, quotation_id);

CREATE TABLE outbox_events (
    id             BIGSERIAL PRIMARY KEY,
    tenant_id      BIGINT      NOT NULL DEFAULT 1,
    aggregate_type TEXT        NOT NULL,
    aggregate_id   TEXT        NOT NULL,
    event_type     TEXT        NOT NULL,
    payload        JSONB       NOT NULL,
    trace_id       TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at   TIMESTAMPTZ,
    attempts       INT         NOT NULL DEFAULT 0,
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

-- +goose Down
DROP TABLE processed_events;
DROP TABLE outbox_events;
DROP TABLE quotation_items;
DROP TABLE quotations;

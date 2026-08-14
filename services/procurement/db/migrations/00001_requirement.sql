-- +goose Up

-- What has to be bought: the full quantity of contracts that took effect.
--
-- Procurement holds no copy of the contract: it learns about one from a Kafka
-- event and keeps only the numbers it needs, snapshotted. A later contract
-- rename or reprice does not reach back into a requirement that has already
-- been acted on, which is the point.
CREATE TABLE purchase_requirements (
    id                  BIGSERIAL     PRIMARY KEY,
    tenant_id           BIGINT        NOT NULL DEFAULT 1,

    -- Where it came from. Ids reference export's tables; there is no foreign
    -- key, because they live in another database on purpose.
    contract_id         BIGINT        NOT NULL,
    contract_no         VARCHAR(50)   NOT NULL,
    -- Which version of the contract asked for this. A change produces a new
    -- version with new line ids, so without this a requirement cannot say
    -- which set of terms it belongs to.
    contract_version_id BIGINT        NOT NULL,
    version_no          INT           NOT NULL DEFAULT 1,
    contract_item_id    BIGINT        NOT NULL,
    customer_name       VARCHAR(200)  NOT NULL DEFAULT '',

    -- Snapshot of what to buy. Copied, not referenced: the contract says what
    -- was sold, and that must not drift if the product is renamed later.
    product_id          BIGINT        NOT NULL,
    sku_id              BIGINT,
    product_code        VARCHAR(100)  NOT NULL DEFAULT '',
    product_name        VARCHAR(200)  NOT NULL DEFAULT '',
    spec                VARCHAR(300)  NOT NULL DEFAULT '',
    uom_id              BIGINT        NOT NULL DEFAULT 0,
    uom_code            VARCHAR(32)   NOT NULL DEFAULT '',

    required_qty        NUMERIC(18,4) NOT NULL CHECK (required_qty > 0),
    ordered_qty         NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (ordered_qty >= 0),
    required_date       DATE,

    source              VARCHAR(32)   NOT NULL DEFAULT 'CONTRACT'
                        CHECK (source IN ('CONTRACT', 'STOCK_ALERT', 'MANUAL')),
    status              VARCHAR(32)   NOT NULL DEFAULT 'PENDING'
                        CHECK (status IN ('PENDING', 'PARTIALLY_ORDERED', 'ORDERED',
                                          'SUPERSEDED', 'CANCELLED')),
    -- Free text for why a requirement was retired, so "it just disappeared"
    -- is never the only explanation available.
    closed_reason       TEXT          NOT NULL DEFAULT '',

    created_at          TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ   NOT NULL DEFAULT now()
);

-- One requirement per contract line. This is also what makes a redelivered
-- Kafka message harmless: the second insert simply conflicts.
CREATE UNIQUE INDEX purchase_requirements_item_idx
    ON purchase_requirements (tenant_id, contract_item_id);

CREATE INDEX purchase_requirements_contract_idx
    ON purchase_requirements (tenant_id, contract_id, status);

-- Consumer dedupe, same table shape as export's. Keyed by consumer group so a
-- second consumer added later cannot swallow this one's events.
CREATE TABLE processed_events (
    event_id       VARCHAR(200) NOT NULL,
    consumer_group VARCHAR(100) NOT NULL,
    processed_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, consumer_group)
);

-- +goose Down
DROP TABLE processed_events;
DROP TABLE purchase_requirements;

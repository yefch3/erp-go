-- +goose Up

-- Pre-sale sourcing is intentionally separate from purchase requirements:
-- an inquiry is not yet a commitment to buy.
CREATE SEQUENCE sourcing_case_no_seq;

CREATE TABLE sourcing_cases (
    id                   BIGSERIAL    PRIMARY KEY,
    tenant_id            BIGINT       NOT NULL DEFAULT 1,
    case_no              VARCHAR(50)  NOT NULL,
    title                VARCHAR(300) NOT NULL DEFAULT '',
    customer_id          BIGINT       NOT NULL DEFAULT 0,
    customer_name        VARCHAR(200) NOT NULL DEFAULT '',
    contact_name         VARCHAR(150) NOT NULL DEFAULT '',
    contact_email        VARCHAR(320) NOT NULL DEFAULT '',
    source_mail_id       BIGINT       NOT NULL DEFAULT 0,
    source_attachment_id BIGINT       NOT NULL DEFAULT 0,
    status               VARCHAR(32)  NOT NULL DEFAULT 'REVIEWING'
                         CHECK (status IN ('REVIEWING', 'SOURCING', 'QUOTES_RECEIVED',
                                           'COSTING', 'CUSTOMER_QUOTE_CREATED', 'CANCELLED')),
    owner_id             BIGINT       NOT NULL DEFAULT 0,
    owner_name           VARCHAR(150) NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, case_no)
);

-- One deliberate conversion of the same mail source produces one case. Text
-- selections have no attachment id and may legitimately produce several
-- cases, so only an attachment source is deduplicated here.
CREATE UNIQUE INDEX sourcing_cases_mail_attachment_idx
    ON sourcing_cases (tenant_id, source_mail_id, source_attachment_id)
    WHERE source_mail_id > 0 AND source_attachment_id > 0;

CREATE TABLE sourcing_lines (
    id                    BIGSERIAL     PRIMARY KEY,
    tenant_id             BIGINT        NOT NULL DEFAULT 1,
    case_id               BIGINT        NOT NULL REFERENCES sourcing_cases(id) ON DELETE CASCADE,
    line_no               INT           NOT NULL,
    raw_text              TEXT          NOT NULL DEFAULT '',
    product               VARCHAR(200)  NOT NULL DEFAULT '',
    material_standard     VARCHAR(200)  NOT NULL DEFAULT '',
    grade                 VARCHAR(120)  NOT NULL DEFAULT '',
    thickness             VARCHAR(100)  NOT NULL DEFAULT '',
    width                 VARCHAR(100)  NOT NULL DEFAULT '',
    length_or_form        VARCHAR(120)  NOT NULL DEFAULT '',
    surface_requirement   VARCHAR(200)  NOT NULL DEFAULT '',
    coating               VARCHAR(150)  NOT NULL DEFAULT '',
    tolerance             VARCHAR(150)  NOT NULL DEFAULT '',
    coil_weight           VARCHAR(120)  NOT NULL DEFAULT '',
    coil_id               VARCHAR(120)  NOT NULL DEFAULT '',
    packaging             VARCHAR(200)  NOT NULL DEFAULT '',
    delivery              VARCHAR(200)  NOT NULL DEFAULT '',
    payment_terms         VARCHAR(200)  NOT NULL DEFAULT '',
    incoterm              VARCHAR(80)   NOT NULL DEFAULT '',
    port                  VARCHAR(150)  NOT NULL DEFAULT '',
    quantity_unit         VARCHAR(50)   NOT NULL DEFAULT '',
    remarks               TEXT          NOT NULL DEFAULT '',
    quantity              NUMERIC(18,4),
    product_id            BIGINT        NOT NULL DEFAULT 0,
    sku_id                BIGINT        NOT NULL DEFAULT 0,
    uom_id                BIGINT        NOT NULL DEFAULT 0,
    decision              VARCHAR(20)   NOT NULL DEFAULT 'PENDING'
                          CHECK (decision IN ('PENDING', 'CONFIRMED', 'NO_MATCH', 'SKIPPED')),
    created_at            TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, case_id, line_no)
);
CREATE INDEX sourcing_cases_status_idx ON sourcing_cases (tenant_id, status, updated_at DESC);
CREATE INDEX sourcing_lines_case_idx ON sourcing_lines (tenant_id, case_id, line_no);

-- +goose Down
DROP TABLE sourcing_lines;
DROP TABLE sourcing_cases;
DROP SEQUENCE sourcing_case_no_seq;

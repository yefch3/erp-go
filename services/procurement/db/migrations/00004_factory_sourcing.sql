-- +goose Up
CREATE SEQUENCE factory_rfq_no_seq;
CREATE SEQUENCE supplier_quote_no_seq;

CREATE TABLE factory_rfqs (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL DEFAULT 1,
    case_id BIGINT NOT NULL REFERENCES sourcing_cases(id) ON DELETE CASCADE,
    rfq_no VARCHAR(50) NOT NULL,
    supplier_id BIGINT NOT NULL,
    supplier_code VARCHAR(100) NOT NULL DEFAULT '',
    supplier_name VARCHAR(200) NOT NULL,
    contact_email VARCHAR(320) NOT NULL DEFAULT '',
    currency VARCHAR(10) NOT NULL DEFAULT 'USD',
    response_due_at DATE,
    status VARCHAR(24) NOT NULL DEFAULT 'DRAFT'
      CHECK (status IN ('DRAFT','SENT','PARTIALLY_QUOTED','QUOTED','CLOSED','CANCELLED')),
    created_by BIGINT NOT NULL DEFAULT 0,
    created_by_name VARCHAR(150) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, rfq_no),
    UNIQUE (tenant_id, case_id, supplier_id)
);

CREATE TABLE factory_rfq_lines (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL DEFAULT 1,
    factory_rfq_id BIGINT NOT NULL REFERENCES factory_rfqs(id) ON DELETE CASCADE,
    sourcing_line_id BIGINT NOT NULL REFERENCES sourcing_lines(id),
    line_no INT NOT NULL,
    qty NUMERIC(18,4) NOT NULL,
    uom_code VARCHAR(50) NOT NULL DEFAULT '',
    spec_snapshot TEXT NOT NULL DEFAULT '',
    UNIQUE (tenant_id, factory_rfq_id, sourcing_line_id)
);

CREATE TABLE supplier_quotes (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL DEFAULT 1,
    factory_rfq_id BIGINT NOT NULL REFERENCES factory_rfqs(id) ON DELETE CASCADE,
    supplier_quote_no VARCHAR(50) NOT NULL,
    quoted_at DATE,
    valid_until DATE,
    currency VARCHAR(10) NOT NULL,
    payment_terms VARCHAR(300) NOT NULL DEFAULT '',
    delivery VARCHAR(200) NOT NULL DEFAULT '',
    remark TEXT NOT NULL DEFAULT '',
    source VARCHAR(24) NOT NULL DEFAULT 'MANUAL'
      CHECK (source IN ('MANUAL','EXCEL_IMPORT','EMAIL_ATTACHMENT')),
    created_by BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, supplier_quote_no)
);

CREATE TABLE supplier_quote_lines (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL DEFAULT 1,
    supplier_quote_id BIGINT NOT NULL REFERENCES supplier_quotes(id) ON DELETE CASCADE,
    sourcing_line_id BIGINT NOT NULL REFERENCES sourcing_lines(id),
    qty NUMERIC(18,4) NOT NULL,
    unit_price NUMERIC(18,4) NOT NULL,
    amount NUMERIC(18,2) NOT NULL,
    moq NUMERIC(18,4),
    lead_time VARCHAR(120) NOT NULL DEFAULT '',
    remark TEXT NOT NULL DEFAULT '',
    UNIQUE (tenant_id, supplier_quote_id, sourcing_line_id)
);

CREATE INDEX factory_rfqs_case_idx ON factory_rfqs (tenant_id, case_id, created_at);
CREATE INDEX supplier_quotes_rfq_idx ON supplier_quotes (tenant_id, factory_rfq_id, created_at);

-- +goose Down
DROP TABLE supplier_quote_lines;
DROP TABLE supplier_quotes;
DROP TABLE factory_rfq_lines;
DROP TABLE factory_rfqs;
DROP SEQUENCE supplier_quote_no_seq;
DROP SEQUENCE factory_rfq_no_seq;

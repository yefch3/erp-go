-- +goose Up
-- C5 邮件模板库: reusable phrases with {{variables}}, applied in one click
-- at compose time. Two layers, exactly like signatures: the company's shared
-- phrasebook (owner_type TENANT) and each person's own (EMPLOYEE).
--
-- lang says which language the CONTENT is written in (zh/en/es, '' when the
-- template reads the same to everyone). It is a filter for the picker, not a
-- grouping key: a quote follow-up may exist in three languages as three
-- independent rows, each edited on its own. Binding them into one "bundle"
-- object would force every edit through a three-tab dialog, and the person
-- who only ever writes in English would still be walked past 中文 and
-- español on the way to their own text.
CREATE TABLE email_templates (
    id          BIGSERIAL    PRIMARY KEY,
    tenant_id   BIGINT       NOT NULL DEFAULT 1,
    owner_type  VARCHAR(16)  NOT NULL DEFAULT 'EMPLOYEE'
        CHECK (owner_type IN ('TENANT','EMPLOYEE')),
    owner_id    BIGINT       NOT NULL DEFAULT 0,
    name        VARCHAR(100) NOT NULL,
    lang        VARCHAR(5)   NOT NULL DEFAULT ''
        CHECK (lang IN ('','zh','en','es')),
    -- The subject travels with the body. 催款 and 报价跟进 each need their
    -- own subject line, and a template that only fills the body leaves the
    -- person retyping the half that decides whether the mail gets opened.
    subject     TEXT         NOT NULL DEFAULT '',
    content     TEXT         NOT NULL,
    body_format VARCHAR(8)   NOT NULL DEFAULT 'HTML'
        CHECK (body_format IN ('TEXT','HTML')),
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX email_templates_list_idx
    ON email_templates (tenant_id, owner_type, owner_id, name);

-- +goose Down
DROP TABLE email_templates;

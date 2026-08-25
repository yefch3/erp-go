-- +goose Up
ALTER TABLE cost_scenarios
    ADD COLUMN version_no INT;

WITH ranked AS (
    SELECT id,
           row_number() OVER (PARTITION BY tenant_id, case_id ORDER BY created_at, id) AS version_no
    FROM cost_scenarios
)
UPDATE cost_scenarios AS scenario
SET version_no = ranked.version_no
FROM ranked
WHERE ranked.id = scenario.id;

ALTER TABLE cost_scenarios
    ALTER COLUMN version_no SET NOT NULL,
    ALTER COLUMN version_no SET DEFAULT 1,
    ADD CONSTRAINT cost_scenarios_version_positive CHECK (version_no > 0),
    ADD CONSTRAINT cost_scenarios_case_version_unique UNIQUE (tenant_id, case_id, version_no);

ALTER TABLE cost_scenarios DROP CONSTRAINT cost_scenarios_status_check;
ALTER TABLE cost_scenarios ALTER COLUMN status TYPE VARCHAR(32);
ALTER TABLE cost_scenarios
    ADD CONSTRAINT cost_scenarios_status_check
    CHECK (status IN ('DRAFT','CONFIRMED','CUSTOMER_QUOTE_CREATED','SUPERSEDED'));

-- 历史上已经关联客户报价的方案，本质上已经越过“仅确认”阶段。
UPDATE cost_scenarios
SET status = 'CUSTOMER_QUOTE_CREATED'
WHERE customer_quotation_id IS NOT NULL AND status = 'CONFIRMED';

-- +goose Down
UPDATE cost_scenarios
SET status = 'CONFIRMED'
WHERE status = 'CUSTOMER_QUOTE_CREATED';

ALTER TABLE cost_scenarios DROP CONSTRAINT cost_scenarios_status_check;
ALTER TABLE cost_scenarios
    ADD CONSTRAINT cost_scenarios_status_check
    CHECK (status IN ('DRAFT','CONFIRMED','SUPERSEDED'));
ALTER TABLE cost_scenarios ALTER COLUMN status TYPE VARCHAR(20);

ALTER TABLE cost_scenarios
    DROP CONSTRAINT cost_scenarios_case_version_unique,
    DROP CONSTRAINT cost_scenarios_version_positive,
    DROP COLUMN version_no;

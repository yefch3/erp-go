-- +goose Up
-- migration-safety: SET NOT NULL cost_scenarios.version_no — 本迁移先回填全部旧数据并设置 DEFAULT 1，旧版容器回滚后仍可继续 INSERT。
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
    ADD CONSTRAINT cost_scenarios_version_positive CHECK (version_no > 0);

-- +goose Down
ALTER TABLE cost_scenarios
    DROP CONSTRAINT cost_scenarios_version_positive,
    DROP COLUMN version_no;

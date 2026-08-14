-- +goose Up
ALTER TABLE quotations ADD COLUMN source_cost_scenario_id BIGINT;
ALTER TABLE quotations ADD COLUMN source_cost_scenario_no VARCHAR(40) NOT NULL DEFAULT '';
ALTER TABLE quotations ADD COLUMN source_sourcing_case_id BIGINT;
ALTER TABLE quotation_items ADD COLUMN source_cost_scenario_line_id BIGINT;
CREATE UNIQUE INDEX quotations_cost_scenario_uidx
  ON quotations(tenant_id, source_cost_scenario_id) WHERE source_cost_scenario_id IS NOT NULL;

-- +goose Down
DROP INDEX quotations_cost_scenario_uidx;
ALTER TABLE quotation_items DROP COLUMN source_cost_scenario_line_id;
ALTER TABLE quotations DROP COLUMN source_sourcing_case_id;
ALTER TABLE quotations DROP COLUMN source_cost_scenario_no;
ALTER TABLE quotations DROP COLUMN source_cost_scenario_id;

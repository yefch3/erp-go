-- +goose Up
-- Informational snapshots only: these do not create demand, bind a live
-- contract, alter payable identifiers or advance shipping/payment workflows.
ALTER TABLE historical_purchase_orders
 ADD COLUMN export_contract_no VARCHAR(50) NOT NULL DEFAULT '',
 ADD COLUMN destination_port VARCHAR(200) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE historical_purchase_orders
 DROP COLUMN destination_port,
 DROP COLUMN export_contract_no;

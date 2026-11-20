-- +goose Up
ALTER TABLE travel_reimbursements ADD COLUMN deleted_at timestamptz;

-- +goose Down
ALTER TABLE travel_reimbursements DROP COLUMN deleted_at;

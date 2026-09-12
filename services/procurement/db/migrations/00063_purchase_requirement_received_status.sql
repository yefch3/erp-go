-- +goose Up
-- D3 adds WAITING_REQUOTE before a real-order requirement is re-quoted.
-- Keep RECEIVED from the existing procurement lifecycle as an allowed state.
ALTER TABLE purchase_requirements DROP CONSTRAINT purchase_requirements_status_check;
ALTER TABLE purchase_requirements ADD CONSTRAINT purchase_requirements_status_check
  CHECK (status IN ('WAITING_REQUOTE', 'PENDING', 'PARTIALLY_ORDERED', 'ORDERED',
                    'RECEIVED', 'SUPERSEDED', 'CANCELLED'));

-- +goose Down
ALTER TABLE purchase_requirements DROP CONSTRAINT purchase_requirements_status_check;
ALTER TABLE purchase_requirements ADD CONSTRAINT purchase_requirements_status_check
  CHECK (status IN ('WAITING_REQUOTE', 'PENDING', 'PARTIALLY_ORDERED', 'ORDERED',
                    'SUPERSEDED', 'CANCELLED'));

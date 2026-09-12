-- +goose Up
-- Financial release creates a real procurement requirement, but D3 stops it
-- before re-quotation. This database state is the guard that prevents an old
-- screen or direct API call from turning it into a purchase order early.
ALTER TABLE purchase_requirements DROP CONSTRAINT purchase_requirements_status_check;
ALTER TABLE purchase_requirements ADD CONSTRAINT purchase_requirements_status_check
  CHECK (status IN ('WAITING_REQUOTE', 'PENDING', 'PARTIALLY_ORDERED', 'ORDERED',
                    'SUPERSEDED', 'CANCELLED'));

-- +goose Down
UPDATE purchase_requirements SET status='PENDING' WHERE status='WAITING_REQUOTE';
ALTER TABLE purchase_requirements DROP CONSTRAINT purchase_requirements_status_check;
ALTER TABLE purchase_requirements ADD CONSTRAINT purchase_requirements_status_check
  CHECK (status IN ('PENDING', 'PARTIALLY_ORDERED', 'ORDERED', 'SUPERSEDED', 'CANCELLED'));

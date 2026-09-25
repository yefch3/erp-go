-- +goose Up
-- Historical audit identities survive permanent removal of unused master records.
ALTER TABLE customer_change_logs DROP CONSTRAINT customer_change_logs_customer_id_fkey;
ALTER TABLE supplier_change_logs DROP CONSTRAINT supplier_change_logs_supplier_id_fkey;
ALTER TABLE port_change_logs DROP CONSTRAINT port_change_logs_port_id_fkey;

-- +goose Down
-- NOT VALID preserves audit entries for records already deleted.
ALTER TABLE customer_change_logs ADD CONSTRAINT customer_change_logs_customer_id_fkey FOREIGN KEY(customer_id) REFERENCES customers(id) NOT VALID;
ALTER TABLE supplier_change_logs ADD CONSTRAINT supplier_change_logs_supplier_id_fkey FOREIGN KEY(supplier_id) REFERENCES suppliers(id) NOT VALID;
ALTER TABLE port_change_logs ADD CONSTRAINT port_change_logs_port_id_fkey FOREIGN KEY(port_id) REFERENCES ports(id) NOT VALID;

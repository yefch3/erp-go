-- +goose Up

ALTER TABLE purchase_order_imports
    DROP CONSTRAINT purchase_order_imports_source_type_check;
ALTER TABLE purchase_order_imports
    ADD CONSTRAINT purchase_order_imports_source_type_check
    CHECK (source_type IN ('MAIL_EXCEL', 'UPLOAD', 'PURCHASE_TEMPLATE'));

-- +goose Down

ALTER TABLE purchase_order_imports
    DROP CONSTRAINT purchase_order_imports_source_type_check;
ALTER TABLE purchase_order_imports
    ADD CONSTRAINT purchase_order_imports_source_type_check
    CHECK (source_type IN ('MAIL_EXCEL', 'UPLOAD'));

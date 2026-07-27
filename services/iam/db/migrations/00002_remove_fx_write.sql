-- +goose Up
-- Business decision 2026-07-27: fx manual entry removed, so the write
-- permission disappears from the dictionary. role_permissions rows cascade.
DELETE FROM permissions WHERE code = 'fx:rate:write';

-- +goose Down
INSERT INTO permissions (code, name, module, menu_path)
VALUES ('fx:rate:write', '手工录入汇率', 'fx', '');

-- +goose Up

-- Stock is identified by product AND sku, not by sku alone.
--
-- A product without variants has no SKU row at all — contract lines for it
-- carry sku_id NULL, which arrives here as 0. Keying stock on sku_id alone
-- meant every such product collided on 0, and worse, a contract for a
-- variant-less product could never match its own stock: it looked for
-- sku_id = 0 and found nothing, so a warehouse full of goods reported a
-- shortage and raised a purchase requirement for stock already on the shelf.
--
-- 0 means "this product has no variants", the same convention export uses
-- when it coalesces a null sku onto the wire.
ALTER TABLE stocks ALTER COLUMN sku_id SET DEFAULT 0;
ALTER TABLE stocks DROP CONSTRAINT stocks_tenant_id_warehouse_id_sku_id_key;
ALTER TABLE stocks ADD CONSTRAINT stocks_tenant_warehouse_product_sku_key
    UNIQUE (tenant_id, warehouse_id, product_id, sku_id);
DROP INDEX stocks_sku_idx;
CREATE INDEX stocks_product_idx ON stocks (tenant_id, product_id, sku_id);

ALTER TABLE stock_reservations ALTER COLUMN sku_id SET DEFAULT 0;

-- +goose Down
DROP INDEX stocks_product_idx;
CREATE INDEX stocks_sku_idx ON stocks (tenant_id, sku_id);
ALTER TABLE stocks DROP CONSTRAINT stocks_tenant_warehouse_product_sku_key;
ALTER TABLE stocks ADD CONSTRAINT stocks_tenant_id_warehouse_id_sku_id_key
    UNIQUE (tenant_id, warehouse_id, sku_id);

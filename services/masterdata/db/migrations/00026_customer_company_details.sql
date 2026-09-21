-- +goose Up
ALTER TABLE customers
    ADD COLUMN company_phone VARCHAR(50) NOT NULL DEFAULT '',
    ADD COLUMN fax_number VARCHAR(50) NOT NULL DEFAULT '',
    ADD COLUMN company_email VARCHAR(200) NOT NULL DEFAULT '',
    ADD COLUMN archive_creator VARCHAR(100) NOT NULL DEFAULT '';

-- Promote legacy Excel extension fields without replacing maintained values.
WITH source AS (
    SELECT v.tenant_id, v.customer_id,
      max(v.value) FILTER (WHERE btrim(d.display_name) = '公司电话') AS phone,
      max(v.value) FILTER (WHERE btrim(d.display_name) IN ('传真号码','传真')) AS fax,
      max(v.value) FILTER (WHERE btrim(d.display_name) IN ('电子信箱','公司邮箱')) AS email,
      max(v.value) FILTER (WHERE btrim(d.display_name) = '建档人') AS creator
    FROM customer_custom_field_values v
    JOIN customer_field_definitions d ON d.tenant_id=v.tenant_id AND d.id=v.field_id
    GROUP BY v.tenant_id, v.customer_id
)
UPDATE customers c SET company_phone=coalesce(s.phone,''), fax_number=coalesce(s.fax,''),
    company_email=coalesce(s.email,''), archive_creator=coalesce(s.creator,'')
FROM source s WHERE c.tenant_id=s.tenant_id AND c.id=s.customer_id;

-- +goose Down
ALTER TABLE customers DROP COLUMN archive_creator, DROP COLUMN company_email,
    DROP COLUMN fax_number, DROP COLUMN company_phone;

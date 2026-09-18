-- +goose Up

-- 00021 allowed arbitrary Excel titles without changing the customers table.
-- Some titles in older imports actually belong to established customer
-- modules. Promote those values once so existing imports behave the same as
-- imports made after the typed mappings were added.

WITH source AS (
    SELECT v.tenant_id, v.customer_id, btrim(v.value) AS value,
           lower(btrim(d.display_name)) AS field_name,
           row_number() OVER (PARTITION BY v.tenant_id, lower(btrim(v.value)) ORDER BY v.customer_id) AS duplicate_rank
    FROM customer_custom_field_values v
    JOIN customer_field_definitions d ON d.id = v.field_id AND d.tenant_id = v.tenant_id
    WHERE btrim(v.value) <> ''
), codes AS (
    SELECT * FROM source WHERE field_name IN ('客户代码', '客户编码', '客户编号')
)
UPDATE customers c
SET code = upper(p.value), updated_at = now()
FROM codes p
WHERE c.tenant_id = p.tenant_id AND c.id = p.customer_id
  AND p.duplicate_rank = 1 AND c.code ~ '^CU-[0-9]+$'
  AND NOT EXISTS (
      SELECT 1 FROM customers other
      WHERE other.tenant_id = c.tenant_id AND upper(other.code) = upper(p.value) AND other.id <> c.id
  );

WITH promoted AS (
    SELECT v.tenant_id, v.customer_id,
           max(btrim(v.value)) FILTER (WHERE lower(btrim(d.display_name)) IN ('通信地址', '公司地址', '办公地址')) AS address,
           max(btrim(v.value)) FILTER (WHERE lower(btrim(d.display_name)) IN ('所属地区', '国家/地区', '国家地区')) AS region,
           max(btrim(v.value)) FILTER (WHERE lower(btrim(d.display_name)) IN ('客户等级', '信用等级')) AS credit_grade
    FROM customer_custom_field_values v
    JOIN customer_field_definitions d ON d.id = v.field_id AND d.tenant_id = v.tenant_id
    GROUP BY v.tenant_id, v.customer_id
)
UPDATE customers c
SET address = CASE WHEN btrim(c.address) = '' THEN coalesce(p.address, '') ELSE c.address END,
    country_code = CASE
        WHEN btrim(c.country_code) <> '' THEN c.country_code
        WHEN p.region ~* '^(中国|china)([·/,， -]|$)' THEN 'CN'
        WHEN p.region ~* '^(美国|美國|united states|usa)([·/,， -]|$)' THEN 'US'
        WHEN p.region ~* '^(加拿大|canada)([·/,， -]|$)' THEN 'CA'
        WHEN p.region ~* '^(英国|英國|united kingdom|uk)([·/,， -]|$)' THEN 'GB'
        WHEN p.region ~* '^(德国|德國|germany)([·/,， -]|$)' THEN 'DE'
        WHEN p.region ~* '^(法国|法國|france)([·/,， -]|$)' THEN 'FR'
        WHEN p.region ~* '^(澳大利亚|澳大利亞|australia)([·/,， -]|$)' THEN 'AU'
        WHEN p.region ~* '^(日本|japan)([·/,， -]|$)' THEN 'JP'
        WHEN p.region ~* '^(韩国|韓國|south korea)([·/,， -]|$)' THEN 'KR'
        WHEN p.region ~* '^(新加坡|singapore)([·/,， -]|$)' THEN 'SG'
        ELSE c.country_code
    END,
    credit_grade = CASE WHEN btrim(c.credit_grade) = '' THEN upper(coalesce(p.credit_grade, '')) ELSE c.credit_grade END,
    credit_graded_at = CASE WHEN btrim(c.credit_grade) = '' AND coalesce(p.credit_grade, '') <> '' THEN now() ELSE c.credit_graded_at END,
    updated_at = now()
FROM promoted p
WHERE c.tenant_id = p.tenant_id AND c.id = p.customer_id
  AND (coalesce(p.address, '') <> '' OR coalesce(p.region, '') <> '' OR coalesce(p.credit_grade, '') <> '');

WITH promoted AS (
    SELECT v.tenant_id, v.customer_id,
           max(btrim(v.value)) FILTER (WHERE lower(btrim(d.display_name)) IN ('联系人', '联系姓名', '联系人姓名')) AS contact_name,
           max(btrim(v.value)) FILTER (WHERE lower(btrim(d.display_name)) IN ('联系人邮箱', '联系邮箱')) AS contact_email,
           max(btrim(v.value)) FILTER (WHERE lower(btrim(d.display_name)) IN ('联系人电话', '联系电话')) AS contact_phone,
           max(btrim(v.value)) FILTER (WHERE lower(btrim(d.display_name)) IN ('联系人手机', '联系人手机号')) AS contact_mobile
    FROM customer_custom_field_values v
    JOIN customer_field_definitions d ON d.id = v.field_id AND d.tenant_id = v.tenant_id
    GROUP BY v.tenant_id, v.customer_id
)
INSERT INTO customer_contacts (
    tenant_id, customer_id, name, email, phone, mobile, is_primary, sort_order,
    created_by, updated_by
)
SELECT p.tenant_id, p.customer_id, coalesce(nullif(p.contact_name, ''), '主要联系人'),
       coalesce(p.contact_email, ''), coalesce(p.contact_phone, ''), coalesce(p.contact_mobile, ''),
       true, 0, 0, 0
FROM promoted p
WHERE (coalesce(p.contact_name, '') <> '' OR coalesce(p.contact_email, '') <> ''
       OR coalesce(p.contact_phone, '') <> '' OR coalesce(p.contact_mobile, '') <> '')
  AND NOT EXISTS (
      SELECT 1 FROM customer_contacts cc
      WHERE cc.tenant_id = p.tenant_id AND cc.customer_id = p.customer_id AND cc.status = 'ACTIVE'
  );

WITH promoted AS (
    SELECT v.tenant_id, v.customer_id,
           max(btrim(v.value)) FILTER (WHERE lower(btrim(d.display_name)) IN ('通信地址', '公司地址', '办公地址')) AS address,
           max(btrim(v.value)) FILTER (WHERE lower(btrim(d.display_name)) IN ('邮政编码', '邮编')) AS postal_code
    FROM customer_custom_field_values v
    JOIN customer_field_definitions d ON d.id = v.field_id AND d.tenant_id = v.tenant_id
    GROUP BY v.tenant_id, v.customer_id
)
INSERT INTO customer_addresses (
    tenant_id, customer_id, address_type, country_code, state, city, postal_code,
    address_line, is_default, sort_order, created_by, updated_by
)
SELECT p.tenant_id, p.customer_id, 'OFFICE', c.country_code, '', '', coalesce(p.postal_code, ''),
       p.address, true, 0, 0, 0
FROM promoted p
JOIN customers c ON c.tenant_id = p.tenant_id AND c.id = p.customer_id
WHERE coalesce(p.address, '') <> ''
  AND NOT EXISTS (
      SELECT 1 FROM customer_addresses ca
      WHERE ca.tenant_id = p.tenant_id AND ca.customer_id = p.customer_id
        AND ca.address_type = 'OFFICE' AND ca.status = 'ACTIVE'
  );

-- Existing imports do not carry an IAM employee id. When the spreadsheet's
-- archive creator and owner are the same person, the importing operator id is
-- the reliable employee id for that name; promote only that unambiguous case.
WITH promoted AS (
    SELECT v.tenant_id, v.customer_id,
           max(btrim(v.value)) FILTER (WHERE lower(btrim(d.display_name)) IN ('分管人', '负责人', '业务负责人')) AS owner_name,
           max(btrim(v.value)) FILTER (WHERE lower(btrim(d.display_name)) IN ('建档人', '创建人')) AS creator_name
    FROM customer_custom_field_values v
    JOIN customer_field_definitions d ON d.id = v.field_id AND d.tenant_id = v.tenant_id
    GROUP BY v.tenant_id, v.customer_id
)
INSERT INTO customer_owners (
    tenant_id, customer_id, employee_id, employee_name, responsibility_code,
    is_primary, created_by, updated_by
)
SELECT p.tenant_id, p.customer_id, c.created_by, p.owner_name, 'SALES', true, c.created_by, c.created_by
FROM promoted p
JOIN customers c ON c.tenant_id = p.tenant_id AND c.id = p.customer_id
WHERE coalesce(p.owner_name, '') <> '' AND p.owner_name = p.creator_name AND c.created_by > 0
  AND NOT EXISTS (
      SELECT 1 FROM customer_owners co
      WHERE co.tenant_id = p.tenant_id AND co.customer_id = p.customer_id AND co.status = 'ACTIVE'
  );

-- Remove only values that now have a canonical destination. Company phone,
-- fax, archive creator and other titles without an ERP home remain visible as
-- extension information.
DELETE FROM customer_custom_field_values v
USING customer_field_definitions d, customers c
WHERE d.id = v.field_id AND d.tenant_id = v.tenant_id
  AND c.id = v.customer_id AND c.tenant_id = v.tenant_id
  AND (
      (lower(btrim(d.display_name)) IN ('客户代码', '客户编码', '客户编号') AND upper(c.code) = upper(btrim(v.value)))
      OR (lower(btrim(d.display_name)) IN ('所属地区', '国家/地区', '国家地区') AND btrim(c.country_code) <> '')
      OR (lower(btrim(d.display_name)) IN ('通信地址', '公司地址', '办公地址') AND btrim(c.address) <> '')
      OR (lower(btrim(d.display_name)) IN ('客户等级', '信用等级') AND btrim(c.credit_grade) <> '')
      OR (lower(btrim(d.display_name)) IN ('联系人', '联系姓名', '联系人姓名', '联系人邮箱', '联系邮箱', '联系人电话', '联系电话', '联系人手机', '联系人手机号')
          AND EXISTS (SELECT 1 FROM customer_contacts cc WHERE cc.tenant_id = v.tenant_id AND cc.customer_id = v.customer_id AND cc.status = 'ACTIVE'))
      OR (lower(btrim(d.display_name)) IN ('邮政编码', '邮编')
          AND EXISTS (SELECT 1 FROM customer_addresses ca WHERE ca.tenant_id = v.tenant_id AND ca.customer_id = v.customer_id AND ca.status = 'ACTIVE'))
      OR (lower(btrim(d.display_name)) IN ('分管人', '负责人', '业务负责人')
          AND EXISTS (SELECT 1 FROM customer_owners co WHERE co.tenant_id = v.tenant_id AND co.customer_id = v.customer_id AND co.status = 'ACTIVE' AND co.employee_name = btrim(v.value)))
  );

-- +goose Down
-- Data promotion is intentionally not reversed: restoring duplicate extension
-- values would require guessing which values originally came from Excel.

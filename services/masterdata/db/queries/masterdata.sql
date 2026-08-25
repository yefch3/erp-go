-- name: CreateCustomer :one
-- country_code is the one that matters now; country keeps whatever free text
-- a caller still sends, and drains to empty as rows are edited through the
-- dropdown. See migration 00007 for why the code and not the name.
INSERT INTO customers (
    tenant_id, code, name, country, country_code, address, currency, payment_term, remark,
    short_name, english_name, customer_type, industry, source, tags, website,
    primary_language, timezone, registered_name, registration_no, tax_id,
    invoice_title, invoice_tax_no, invoice_remark, payment_days, credit_limit_minor,
    credit_currency, credit_status, business_status, created_by, updated_by
)
VALUES (
    sqlc.arg(tenant_id), sqlc.arg(code), sqlc.arg(name), sqlc.arg(country),
    sqlc.arg(country_code)::text, sqlc.arg(address), sqlc.arg(currency),
    sqlc.arg(payment_term), sqlc.arg(remark), sqlc.arg(short_name), sqlc.arg(english_name),
    sqlc.arg(customer_type), sqlc.arg(industry), sqlc.arg(source), sqlc.arg(tags)::text[],
    sqlc.arg(website), sqlc.arg(primary_language), sqlc.arg(timezone),
    sqlc.arg(registered_name), sqlc.arg(registration_no), sqlc.arg(tax_id),
    sqlc.arg(invoice_title), sqlc.arg(invoice_tax_no), sqlc.arg(invoice_remark),
    sqlc.arg(payment_days), sqlc.arg(credit_limit_minor), sqlc.arg(credit_currency),
    sqlc.arg(credit_status), sqlc.arg(business_status), sqlc.arg(operator_id), sqlc.arg(operator_id)
)
RETURNING *;

-- name: GetCustomer :one
SELECT * FROM customers WHERE tenant_id = $1 AND id = $2;

-- name: ListCustomers :many
SELECT c.*,
  COALESCE((SELECT cc.name FROM customer_contacts cc
            WHERE cc.tenant_id = c.tenant_id AND cc.customer_id = c.id AND cc.status = 'ACTIVE'
            ORDER BY cc.is_primary DESC, cc.sort_order, cc.id LIMIT 1), ''::text)::text AS primary_contact_name,
  COALESCE((SELECT string_agg(co.employee_name, '、' ORDER BY co.id) FROM customer_owners co
            WHERE co.tenant_id = c.tenant_id AND co.customer_id = c.id AND co.status = 'ACTIVE'), ''::text)::text AS owner_names,
  count(*) OVER () AS total
FROM customers c
WHERE c.tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.arg(status)::text = 'ALL' OR c.status = 'ACTIVE')
  AND (
      sqlc.arg(country_code)::text = ''
      OR (sqlc.arg(country_code)::text = '__UNCLASSIFIED__' AND btrim(c.country_code) = '')
      OR c.country_code = sqlc.arg(country_code)::text
  )
  AND (sqlc.arg(customer_type)::text = '' OR c.customer_type = sqlc.arg(customer_type))
  AND (sqlc.arg(business_status)::text = '' OR c.business_status = sqlc.arg(business_status))
  AND (sqlc.arg(tag)::text = '' OR sqlc.arg(tag) = ANY(c.tags))
  AND (sqlc.arg(owner_employee_id)::bigint = 0 OR EXISTS (
      SELECT 1 FROM customer_owners co WHERE co.tenant_id = c.tenant_id AND co.customer_id = c.id
        AND co.employee_id = sqlc.arg(owner_employee_id) AND co.status = 'ACTIVE'
  ))
  AND (sqlc.arg(keyword)::text = '' OR c.name ILIKE '%' || sqlc.arg(keyword) || '%' OR c.code ILIKE '%' || sqlc.arg(keyword) || '%')
ORDER BY c.id DESC
LIMIT sqlc.arg(limit_count) OFFSET sqlc.arg(offset_count);

-- name: UpdateCustomer :one
UPDATE customers
SET name = sqlc.arg(name), country = sqlc.arg(country),
    country_code = sqlc.arg(country_code)::text, address = sqlc.arg(address),
    currency = sqlc.arg(currency), payment_term = sqlc.arg(payment_term),
    remark = sqlc.arg(remark), updated_by = sqlc.arg(operator_id), updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id)
RETURNING *;

-- name: UpdateCustomerProfile :one
-- 详细资料独立更新，避免旧版基础信息表单因未携带新字段而清空已有资料。
UPDATE customers
SET short_name = sqlc.arg(short_name), english_name = sqlc.arg(english_name),
    customer_type = sqlc.arg(customer_type), industry = sqlc.arg(industry),
    source = sqlc.arg(source), tags = sqlc.arg(tags)::text[], website = sqlc.arg(website),
    primary_language = sqlc.arg(primary_language), timezone = sqlc.arg(timezone),
    registered_name = sqlc.arg(registered_name), registration_no = sqlc.arg(registration_no),
    tax_id = sqlc.arg(tax_id), invoice_title = sqlc.arg(invoice_title),
    invoice_tax_no = sqlc.arg(invoice_tax_no), invoice_remark = sqlc.arg(invoice_remark),
    payment_days = sqlc.arg(payment_days), credit_limit_minor = sqlc.arg(credit_limit_minor),
    credit_currency = sqlc.arg(credit_currency), credit_status = sqlc.arg(credit_status),
    business_status = sqlc.arg(business_status), updated_by = sqlc.arg(operator_id),
    updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id)
RETURNING *;

-- name: DeactivateCustomer :execrows
UPDATE customers SET status = 'INACTIVE', business_status = 'INACTIVE', updated_by = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND status = 'ACTIVE';

-- name: DeleteCustomerContacts :exec
DELETE FROM customer_contacts WHERE tenant_id = $1 AND customer_id = $2;

-- name: AddCustomerContact :exec
INSERT INTO customer_contacts (tenant_id, customer_id, name, title, email, phone, is_primary, sort_order)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListCustomerContacts :many
SELECT * FROM customer_contacts
WHERE tenant_id = $1 AND customer_id = $2
ORDER BY sort_order, id;

-- name: ListCustomerContactsDetailed :many
SELECT * FROM customer_contacts
WHERE tenant_id = sqlc.arg(tenant_id) AND customer_id = sqlc.arg(customer_id)
  AND (sqlc.arg(status)::text = 'ALL' OR status = 'ACTIVE')
ORDER BY status, is_primary DESC, sort_order, id;

-- name: GetCustomerContact :one
SELECT * FROM customer_contacts
WHERE tenant_id = sqlc.arg(tenant_id) AND customer_id = sqlc.arg(customer_id)
  AND id = sqlc.arg(id);

-- name: ClearPrimaryCustomerContact :exec
UPDATE customer_contacts
SET is_primary = false, updated_by = sqlc.arg(operator_id), updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND customer_id = sqlc.arg(customer_id)
  AND status = 'ACTIVE' AND is_primary;

-- name: CreateCustomerContact :one
INSERT INTO customer_contacts (
    tenant_id, customer_id, name, department, title, email, phone, mobile,
    instant_messaging, language, remark, is_primary, sort_order, email_permission,
    email_categories, created_by, updated_by
) VALUES (
    sqlc.arg(tenant_id), sqlc.arg(customer_id), sqlc.arg(name), sqlc.arg(department),
    sqlc.arg(title), sqlc.arg(email), sqlc.arg(phone), sqlc.arg(mobile),
    sqlc.arg(instant_messaging), sqlc.arg(language), sqlc.arg(remark),
    sqlc.arg(is_primary), sqlc.arg(sort_order), sqlc.arg(email_permission),
    sqlc.arg(email_categories), sqlc.arg(operator_id), sqlc.arg(operator_id)
)
RETURNING *;

-- name: UpdateCustomerContact :one
UPDATE customer_contacts
SET name = sqlc.arg(name), department = sqlc.arg(department), title = sqlc.arg(title),
    email = sqlc.arg(email), phone = sqlc.arg(phone), mobile = sqlc.arg(mobile),
    instant_messaging = sqlc.arg(instant_messaging), language = sqlc.arg(language),
    remark = sqlc.arg(remark), is_primary = sqlc.arg(is_primary),
    sort_order = sqlc.arg(sort_order), email_permission = sqlc.arg(email_permission),
    email_categories = sqlc.arg(email_categories), updated_by = sqlc.arg(operator_id), updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND customer_id = sqlc.arg(customer_id)
  AND id = sqlc.arg(id) AND status = 'ACTIVE'
RETURNING *;

-- name: DeactivateCustomerContact :execrows
UPDATE customer_contacts
SET status = 'INACTIVE', is_primary = false,
    updated_by = sqlc.arg(operator_id), updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND customer_id = sqlc.arg(customer_id)
  AND id = sqlc.arg(id) AND status = 'ACTIVE';

-- name: ListCustomerOwners :many
SELECT * FROM customer_owners
WHERE tenant_id = sqlc.arg(tenant_id) AND customer_id = sqlc.arg(customer_id)
  AND (sqlc.arg(status)::text = 'ALL' OR status = 'ACTIVE')
ORDER BY status, is_primary DESC, responsibility_code, id;

-- name: ListCustomerIDsOwnedByEmployees :many
SELECT DISTINCT customer_id
FROM customer_owners
WHERE tenant_id = sqlc.arg(tenant_id)
  AND employee_id = ANY(sqlc.arg(employee_ids)::bigint[])
  AND status = 'ACTIVE'
  AND (start_date IS NULL OR start_date <= CURRENT_DATE)
  AND (end_date IS NULL OR end_date >= CURRENT_DATE)
ORDER BY customer_id;

-- name: CountActiveCustomerOwners :one
SELECT count(*) FROM customer_owners
WHERE tenant_id = sqlc.arg(tenant_id) AND customer_id = sqlc.arg(customer_id)
  AND status = 'ACTIVE';

-- name: CreateCustomerOwner :one
INSERT INTO customer_owners (
    tenant_id, customer_id, employee_id, employee_name, responsibility_code,
    start_date, end_date, is_primary, created_by, updated_by
) VALUES (
    sqlc.arg(tenant_id), sqlc.arg(customer_id), sqlc.arg(employee_id), sqlc.arg(employee_name),
    sqlc.arg(responsibility_code), sqlc.narg(start_date), sqlc.narg(end_date),
    sqlc.arg(is_primary), sqlc.arg(operator_id), sqlc.arg(operator_id)
)
RETURNING *;

-- name: GetCustomerOwner :one
SELECT * FROM customer_owners
WHERE tenant_id = sqlc.arg(tenant_id) AND customer_id = sqlc.arg(customer_id)
  AND id = sqlc.arg(id);

-- name: ClearPrimaryCustomerOwners :exec
UPDATE customer_owners
SET is_primary = false, updated_by = sqlc.arg(operator_id), updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND customer_id = sqlc.arg(customer_id)
  AND status = 'ACTIVE' AND is_primary = true
  AND id <> sqlc.arg(exclude_id);

-- name: UpdateCustomerOwner :one
UPDATE customer_owners
SET responsibility_code = sqlc.arg(responsibility_code),
    start_date = sqlc.narg(start_date),
    end_date = sqlc.narg(end_date),
    is_primary = sqlc.arg(is_primary),
    updated_by = sqlc.arg(operator_id),
    updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND customer_id = sqlc.arg(customer_id)
  AND id = sqlc.arg(id) AND status = 'ACTIVE'
RETURNING *;

-- name: DeactivateCustomerOwner :execrows
UPDATE customer_owners
SET status = 'INACTIVE', is_primary = false,
    end_date = COALESCE(sqlc.narg(end_date), end_date, CURRENT_DATE),
    updated_by = sqlc.arg(operator_id), updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND customer_id = sqlc.arg(customer_id)
  AND id = sqlc.arg(id) AND status = 'ACTIVE';

-- name: InsertCustomerChangeLog :one
INSERT INTO customer_change_logs (
    tenant_id, customer_id, action, section, summary, before_data, after_data,
    operator_id, operator_name
) VALUES (
    sqlc.arg(tenant_id), sqlc.arg(customer_id), sqlc.arg(action), sqlc.arg(section),
    sqlc.arg(summary), sqlc.arg(before_data)::jsonb, sqlc.arg(after_data)::jsonb,
    sqlc.arg(operator_id), sqlc.arg(operator_name)
)
RETURNING *;

-- name: ListCustomerChangeLogs :many
SELECT * FROM customer_change_logs
WHERE tenant_id = sqlc.arg(tenant_id) AND customer_id = sqlc.arg(customer_id)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(limit_count) OFFSET sqlc.arg(offset_count);

-- name: CountCustomerChangeLogs :one
SELECT count(*) FROM customer_change_logs
WHERE tenant_id = sqlc.arg(tenant_id) AND customer_id = sqlc.arg(customer_id);

-- name: CustomerDuplicateCandidates :many
SELECT DISTINCT c.id, c.code, c.name, c.tax_id,
       coalesce((SELECT cc.email FROM customer_contacts cc
                 WHERE cc.tenant_id = c.tenant_id AND cc.customer_id = c.id
                   AND cc.status = 'ACTIVE' AND cc.email <> ''
                 ORDER BY cc.is_primary DESC, cc.id LIMIT 1), '')::text AS email
FROM customers c
WHERE c.tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.arg(exclude_id)::bigint = 0 OR c.id <> sqlc.arg(exclude_id))
  AND (
    (sqlc.arg(name)::text <> '' AND (
      lower(btrim(c.name)) = lower(btrim(sqlc.arg(name))) OR
      lower(btrim(c.name)) LIKE '%' || lower(btrim(sqlc.arg(name))) || '%' OR
      lower(btrim(sqlc.arg(name))) LIKE '%' || lower(btrim(c.name)) || '%'
    ))
    OR (sqlc.arg(tax_id)::text <> '' AND lower(btrim(c.tax_id)) = lower(btrim(sqlc.arg(tax_id))))
    OR (sqlc.arg(email)::text <> '' AND EXISTS (
      SELECT 1 FROM customer_contacts cc
      WHERE cc.tenant_id = c.tenant_id AND cc.customer_id = c.id
        AND lower(btrim(cc.email)) = lower(btrim(sqlc.arg(email)))
    ))
  )
ORDER BY c.id
LIMIT 10;

-- name: SupplierDuplicateCandidates :many
SELECT id, code, coalesce(nullif(name_zh, ''), nullif(name_en, ''), name)::text AS name,
       tax_id, contact_email
FROM suppliers
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.arg(exclude_id)::bigint = 0 OR id <> sqlc.arg(exclude_id))
  AND (
    (sqlc.arg(name)::text <> '' AND (
      lower(btrim(coalesce(nullif(name_zh, ''), nullif(name_en, ''), name))) = lower(btrim(sqlc.arg(name))) OR
      lower(btrim(coalesce(nullif(name_zh, ''), nullif(name_en, ''), name))) LIKE '%' || lower(btrim(sqlc.arg(name))) || '%' OR
      lower(btrim(sqlc.arg(name))) LIKE '%' || lower(btrim(coalesce(nullif(name_zh, ''), nullif(name_en, ''), name))) || '%'
    ))
    OR (sqlc.arg(tax_id)::text <> '' AND lower(btrim(tax_id)) = lower(btrim(sqlc.arg(tax_id))))
    OR (sqlc.arg(email)::text <> '' AND lower(btrim(contact_email)) = lower(btrim(sqlc.arg(email))))
  )
ORDER BY id
LIMIT 10;

-- name: FactoryDuplicateCandidates :many
SELECT f.id, f.code, f.supplier_id,
       coalesce(nullif(s.name_zh, ''), nullif(s.name_en, ''), s.name)::text AS supplier_name,
       coalesce(nullif(f.name_zh, ''), f.name_en)::text AS name, f.address
FROM factories f
JOIN suppliers s ON s.tenant_id = f.tenant_id AND s.id = f.supplier_id
WHERE f.tenant_id = sqlc.arg(tenant_id)
  AND f.supplier_id = sqlc.arg(supplier_id)
  AND (sqlc.arg(exclude_id)::bigint = 0 OR f.id <> sqlc.arg(exclude_id))
  AND sqlc.arg(name)::text <> ''
  AND (
    lower(btrim(coalesce(nullif(f.name_zh, ''), f.name_en))) = lower(btrim(sqlc.arg(name))) OR
    lower(btrim(coalesce(nullif(f.name_zh, ''), f.name_en))) LIKE '%' || lower(btrim(sqlc.arg(name))) || '%' OR
    lower(btrim(sqlc.arg(name))) LIKE '%' || lower(btrim(coalesce(nullif(f.name_zh, ''), f.name_en))) || '%'
  )
  AND (
    sqlc.arg(address)::text = '' OR f.address = '' OR
    lower(regexp_replace(btrim(f.address), '[[:space:]]+', '', 'g')) =
      lower(regexp_replace(btrim(sqlc.arg(address)), '[[:space:]]+', '', 'g'))
  )
ORDER BY f.id
LIMIT 10;

-- name: CustomerCodeExists :one
SELECT EXISTS(
    SELECT 1 FROM customers WHERE tenant_id = sqlc.arg(tenant_id) AND code = sqlc.arg(code)
);

-- name: ListCustomerAddresses :many
SELECT * FROM customer_addresses
WHERE tenant_id = sqlc.arg(tenant_id)
  AND customer_id = sqlc.arg(customer_id)
  AND (sqlc.arg(status)::text = 'ALL' OR status = 'ACTIVE')
ORDER BY address_type, sort_order, id;

-- name: GetCustomerAddress :one
SELECT * FROM customer_addresses
WHERE tenant_id = sqlc.arg(tenant_id) AND customer_id = sqlc.arg(customer_id)
  AND id = sqlc.arg(id);

-- name: ClearDefaultCustomerAddress :exec
UPDATE customer_addresses
SET is_default = false, updated_by = sqlc.arg(operator_id), updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND customer_id = sqlc.arg(customer_id)
  AND address_type = sqlc.arg(address_type) AND status = 'ACTIVE' AND is_default;

-- name: CreateCustomerAddress :one
INSERT INTO customer_addresses (
    tenant_id, customer_id, address_type, country_code, state, city, postal_code,
    address_line, is_default, sort_order, created_by, updated_by
) VALUES (
    sqlc.arg(tenant_id), sqlc.arg(customer_id), sqlc.arg(address_type),
    sqlc.arg(country_code), sqlc.arg(state), sqlc.arg(city), sqlc.arg(postal_code),
    sqlc.arg(address_line), sqlc.arg(is_default), sqlc.arg(sort_order),
    sqlc.arg(operator_id), sqlc.arg(operator_id)
)
RETURNING *;

-- name: UpdateCustomerAddress :one
UPDATE customer_addresses
SET address_type = sqlc.arg(address_type), country_code = sqlc.arg(country_code),
    state = sqlc.arg(state), city = sqlc.arg(city), postal_code = sqlc.arg(postal_code),
    address_line = sqlc.arg(address_line), is_default = sqlc.arg(is_default),
    sort_order = sqlc.arg(sort_order), updated_by = sqlc.arg(operator_id), updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND customer_id = sqlc.arg(customer_id)
  AND id = sqlc.arg(id) AND status = 'ACTIVE'
RETURNING *;

-- name: DeactivateCustomerAddress :execrows
UPDATE customer_addresses
SET status = 'INACTIVE', is_default = false,
    updated_by = sqlc.arg(operator_id), updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND customer_id = sqlc.arg(customer_id)
  AND id = sqlc.arg(id) AND status = 'ACTIVE';

-- name: CreateSupplier :one
INSERT INTO suppliers (
  tenant_id, code, name, name_zh, name_en, short_name, country, country_code,
  address, registered_address, tax_id, currency, payment_term, business_types,
  contact_name, contact_phone, contact_email, remark, created_by, updated_by
)
VALUES (
  sqlc.arg(tenant_id), sqlc.arg(code), sqlc.arg(name), sqlc.arg(name_zh), sqlc.arg(name_en),
  sqlc.arg(short_name), sqlc.arg(country), sqlc.arg(country_code), sqlc.arg(address),
  sqlc.arg(registered_address), sqlc.arg(tax_id), sqlc.arg(currency), sqlc.arg(payment_term),
  sqlc.arg(business_types), sqlc.arg(contact_name), sqlc.arg(contact_phone),
  sqlc.arg(contact_email), sqlc.arg(remark), sqlc.arg(operator_id), sqlc.arg(operator_id)
)
RETURNING *;

-- name: GetSupplier :one
SELECT * FROM suppliers WHERE tenant_id = $1 AND id = $2;

-- name: GetSupplierByCode :one
SELECT * FROM suppliers
WHERE tenant_id = sqlc.arg(tenant_id) AND upper(code) = upper(sqlc.arg(code))
LIMIT 1;

-- name: SupplierCodeExists :one
SELECT EXISTS (
  SELECT 1 FROM suppliers
  WHERE tenant_id = sqlc.arg(tenant_id) AND upper(code) = upper(sqlc.arg(code))
);

-- name: ListSuppliers :many
SELECT s.*,
       (SELECT count(*) FROM factories f WHERE f.tenant_id = s.tenant_id AND f.supplier_id = s.id AND f.status <> 'INACTIVE') AS factory_count,
       COALESCE((SELECT string_agg(so.employee_name, '、' ORDER BY so.is_primary DESC, so.id)
          FROM supplier_owners so WHERE so.tenant_id = s.tenant_id AND so.supplier_id = s.id AND so.status = 'ACTIVE'), ''::text)::text AS owner_names,
       count(*) OVER () AS total
FROM suppliers s
WHERE s.tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.arg(status)::text = 'ALL' OR (sqlc.arg(status)::text = '' AND s.status = 'ACTIVE') OR s.status = sqlc.arg(status)::text)
  AND (sqlc.arg(country_code)::text = '' OR s.country_code = sqlc.arg(country_code)::text)
  AND (sqlc.arg(business_type)::text = '' OR sqlc.arg(business_type)::text = ANY(s.business_types))
  AND (sqlc.arg(owner_id)::bigint = 0 OR EXISTS (
        SELECT 1 FROM supplier_owners so WHERE so.tenant_id = s.tenant_id AND so.supplier_id = s.id
          AND so.employee_id = sqlc.arg(owner_id)::bigint AND so.status = 'ACTIVE'))
  AND (sqlc.arg(keyword)::text = '' OR s.name ILIKE '%' || sqlc.arg(keyword) || '%'
       OR s.name_zh ILIKE '%' || sqlc.arg(keyword) || '%' OR s.name_en ILIKE '%' || sqlc.arg(keyword) || '%'
       OR s.short_name ILIKE '%' || sqlc.arg(keyword) || '%' OR s.code ILIKE '%' || sqlc.arg(keyword) || '%')
ORDER BY s.id DESC
LIMIT sqlc.arg(page_size) OFFSET sqlc.arg(page_offset);

-- name: UpdateSupplier :one
UPDATE suppliers
SET name = sqlc.arg(name), name_zh = sqlc.arg(name_zh), name_en = sqlc.arg(name_en),
    short_name = sqlc.arg(short_name), country = sqlc.arg(country), country_code = sqlc.arg(country_code),
    address = sqlc.arg(address), registered_address = sqlc.arg(registered_address), tax_id = sqlc.arg(tax_id),
    currency = sqlc.arg(currency), payment_term = sqlc.arg(payment_term), business_types = sqlc.arg(business_types),
    contact_name = sqlc.arg(contact_name), contact_phone = sqlc.arg(contact_phone),
    contact_email = sqlc.arg(contact_email), remark = sqlc.arg(remark),
    updated_by = sqlc.arg(operator_id), updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id)
RETURNING *;

-- name: ListSupplierCountries :many
SELECT country_code, count(*) AS supplier_count
FROM suppliers
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.arg(include_inactive)::boolean OR status = 'ACTIVE')
GROUP BY country_code
ORDER BY country_code;

-- name: ListSupplierContacts :many
SELECT * FROM supplier_contacts
WHERE tenant_id = sqlc.arg(tenant_id) AND supplier_id = sqlc.arg(supplier_id)
  AND (sqlc.arg(include_inactive)::boolean OR status = 'ACTIVE')
ORDER BY is_primary DESC, id;

-- name: CreateSupplierContact :one
INSERT INTO supplier_contacts (tenant_id, supplier_id, name, department, title, phone, email, is_primary, remark, created_by, updated_by)
VALUES (sqlc.arg(tenant_id), sqlc.arg(supplier_id), sqlc.arg(name), sqlc.arg(department), sqlc.arg(title),
        sqlc.arg(phone), sqlc.arg(email), sqlc.arg(is_primary), sqlc.arg(remark), sqlc.arg(operator_id), sqlc.arg(operator_id))
RETURNING *;

-- name: UpdateSupplierContact :one
UPDATE supplier_contacts SET name=sqlc.arg(name), department=sqlc.arg(department), title=sqlc.arg(title),
 phone=sqlc.arg(phone), email=sqlc.arg(email), is_primary=sqlc.arg(is_primary), remark=sqlc.arg(remark),
 updated_by=sqlc.arg(operator_id), updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND supplier_id=sqlc.arg(supplier_id) AND id=sqlc.arg(id)
RETURNING *;

-- name: DeactivateSupplierContact :execrows
UPDATE supplier_contacts SET status='INACTIVE', is_primary=false, updated_by=sqlc.arg(operator_id), updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND supplier_id=sqlc.arg(supplier_id) AND id=sqlc.arg(id) AND status='ACTIVE';

-- name: ClearSupplierPrimaryContact :exec
UPDATE supplier_contacts SET is_primary=false, updated_by=sqlc.arg(operator_id), updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND supplier_id=sqlc.arg(supplier_id) AND status='ACTIVE' AND id<>sqlc.arg(except_id);

-- name: ListSupplierOwners :many
SELECT * FROM supplier_owners
WHERE tenant_id=sqlc.arg(tenant_id) AND supplier_id=sqlc.arg(supplier_id)
  AND (sqlc.arg(include_inactive)::boolean OR status='ACTIVE')
ORDER BY is_primary DESC, id;

-- name: CreateSupplierOwner :one
INSERT INTO supplier_owners (tenant_id, supplier_id, employee_id, employee_name, responsibility_code, is_primary, start_date, end_date, created_by, updated_by)
VALUES (sqlc.arg(tenant_id), sqlc.arg(supplier_id), sqlc.arg(employee_id), sqlc.arg(employee_name),
 sqlc.arg(responsibility_code), sqlc.arg(is_primary), sqlc.narg(start_date), sqlc.narg(end_date), sqlc.arg(operator_id), sqlc.arg(operator_id))
RETURNING *;

-- name: UpdateSupplierOwner :one
UPDATE supplier_owners SET employee_id=sqlc.arg(employee_id), employee_name=sqlc.arg(employee_name),
 responsibility_code=sqlc.arg(responsibility_code), is_primary=sqlc.arg(is_primary),
 start_date=sqlc.narg(start_date), end_date=sqlc.narg(end_date), updated_by=sqlc.arg(operator_id), updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND supplier_id=sqlc.arg(supplier_id) AND id=sqlc.arg(id)
RETURNING *;

-- name: DeactivateSupplierOwner :execrows
UPDATE supplier_owners SET status='INACTIVE', is_primary=false, updated_by=sqlc.arg(operator_id), updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND supplier_id=sqlc.arg(supplier_id) AND id=sqlc.arg(id) AND status='ACTIVE';

-- name: ClearSupplierPrimaryOwner :exec
UPDATE supplier_owners SET is_primary=false, updated_by=sqlc.arg(operator_id), updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND supplier_id=sqlc.arg(supplier_id) AND status='ACTIVE' AND id<>sqlc.arg(except_id);

-- name: CreateSupplierChange :exec
INSERT INTO supplier_change_logs (tenant_id, supplier_id, action, section, summary, before_data, after_data, operator_id, operator_name)
VALUES (sqlc.arg(tenant_id), sqlc.arg(supplier_id), sqlc.arg(action), sqlc.arg(section), sqlc.arg(summary),
 sqlc.arg(before_data), sqlc.arg(after_data), sqlc.arg(operator_id), sqlc.arg(operator_name));

-- name: ListSupplierChanges :many
SELECT * FROM supplier_change_logs WHERE tenant_id=sqlc.arg(tenant_id) AND supplier_id=sqlc.arg(supplier_id)
ORDER BY created_at DESC, id DESC LIMIT 200;

-- name: ListFactories :many
SELECT f.*, s.code AS supplier_code, s.name AS supplier_name,
 COALESCE((SELECT string_agg(fo.employee_name, '、' ORDER BY fo.is_primary DESC, fo.id)
  FROM factory_owners fo WHERE fo.tenant_id=f.tenant_id AND fo.factory_id=f.id AND fo.status='ACTIVE'), ''::text)::text AS owner_names,
 count(*) OVER () AS total
FROM factories f JOIN suppliers s ON s.tenant_id=f.tenant_id AND s.id=f.supplier_id
WHERE f.tenant_id=sqlc.arg(tenant_id)
 AND (sqlc.arg(status)::text='ALL' OR (sqlc.arg(status)::text='' AND f.status<>'INACTIVE') OR f.status=sqlc.arg(status)::text)
 AND (sqlc.arg(country_code)::text='' OR f.country_code=sqlc.arg(country_code)::text)
 AND (sqlc.arg(city)::text='' OR f.city ILIKE '%' || sqlc.arg(city)::text || '%')
 AND (sqlc.arg(supplier_id)::bigint=0 OR f.supplier_id=sqlc.arg(supplier_id)::bigint)
 AND (sqlc.arg(owner_id)::bigint=0 OR EXISTS (SELECT 1 FROM factory_owners fo WHERE fo.tenant_id=f.tenant_id AND fo.factory_id=f.id AND fo.employee_id=sqlc.arg(owner_id)::bigint AND fo.status='ACTIVE'))
 AND (sqlc.arg(product_category)::text='' OR EXISTS (
   SELECT 1 FROM factory_capabilities fc
   WHERE fc.tenant_id=f.tenant_id AND fc.factory_id=f.id
     AND fc.product_category ILIKE '%' || sqlc.arg(product_category)::text || '%'))
 AND (sqlc.arg(keyword)::text='' OR f.code ILIKE '%' || sqlc.arg(keyword)::text || '%' OR f.name_zh ILIKE '%' || sqlc.arg(keyword)::text || '%' OR f.name_en ILIKE '%' || sqlc.arg(keyword)::text || '%')
ORDER BY f.id DESC LIMIT sqlc.arg(page_size) OFFSET sqlc.arg(page_offset);

-- name: GetFactory :one
SELECT * FROM factories WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id);

-- name: FactoryCodeExists :one
SELECT EXISTS (
  SELECT 1 FROM factories
  WHERE tenant_id = sqlc.arg(tenant_id) AND upper(code) = upper(sqlc.arg(code))
);

-- name: CreateFactory :one
INSERT INTO factories (tenant_id,supplier_id,code,name_zh,name_en,short_name,country_code,timezone,state_province,city,district,postal_code,address,status,remark,created_by,updated_by)
VALUES (sqlc.arg(tenant_id),sqlc.arg(supplier_id),sqlc.arg(code),sqlc.arg(name_zh),sqlc.arg(name_en),sqlc.arg(short_name),
 sqlc.arg(country_code),sqlc.arg(timezone),sqlc.arg(state_province),sqlc.arg(city),sqlc.arg(district),sqlc.arg(postal_code),
 sqlc.arg(address),sqlc.arg(status),sqlc.arg(remark),sqlc.arg(operator_id),sqlc.arg(operator_id)) RETURNING *;

-- name: UpdateFactory :one
UPDATE factories SET supplier_id=sqlc.arg(supplier_id),name_zh=sqlc.arg(name_zh),name_en=sqlc.arg(name_en),short_name=sqlc.arg(short_name),
 country_code=sqlc.arg(country_code),timezone=sqlc.arg(timezone),state_province=sqlc.arg(state_province),city=sqlc.arg(city),district=sqlc.arg(district),
 postal_code=sqlc.arg(postal_code),address=sqlc.arg(address),status=sqlc.arg(status),remark=sqlc.arg(remark),updated_by=sqlc.arg(operator_id),updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) RETURNING *;

-- name: ListFactoryCountries :many
SELECT country_code,count(*) AS factory_count FROM factories WHERE tenant_id=sqlc.arg(tenant_id)
 AND (sqlc.arg(include_inactive)::boolean OR status<>'INACTIVE') GROUP BY country_code ORDER BY country_code;

-- name: ListFactoryContacts :many
SELECT * FROM factory_contacts WHERE tenant_id=sqlc.arg(tenant_id) AND factory_id=sqlc.arg(factory_id)
 AND (sqlc.arg(include_inactive)::boolean OR status='ACTIVE') ORDER BY is_primary DESC,id;
-- name: CreateFactoryContact :one
INSERT INTO factory_contacts (tenant_id,factory_id,name,department,title,phone,email,is_primary,remark,created_by,updated_by)
VALUES (sqlc.arg(tenant_id),sqlc.arg(factory_id),sqlc.arg(name),sqlc.arg(department),sqlc.arg(title),sqlc.arg(phone),sqlc.arg(email),sqlc.arg(is_primary),sqlc.arg(remark),sqlc.arg(operator_id),sqlc.arg(operator_id)) RETURNING *;
-- name: UpdateFactoryContact :one
UPDATE factory_contacts SET name=sqlc.arg(name),department=sqlc.arg(department),title=sqlc.arg(title),phone=sqlc.arg(phone),email=sqlc.arg(email),is_primary=sqlc.arg(is_primary),remark=sqlc.arg(remark),updated_by=sqlc.arg(operator_id),updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND factory_id=sqlc.arg(factory_id) AND id=sqlc.arg(id) RETURNING *;
-- name: DeactivateFactoryContact :execrows
UPDATE factory_contacts SET status='INACTIVE',is_primary=false,updated_by=sqlc.arg(operator_id),updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND factory_id=sqlc.arg(factory_id) AND id=sqlc.arg(id) AND status='ACTIVE';
-- name: ClearFactoryPrimaryContact :exec
UPDATE factory_contacts SET is_primary=false,updated_by=sqlc.arg(operator_id),updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND factory_id=sqlc.arg(factory_id) AND status='ACTIVE' AND id<>sqlc.arg(except_id);

-- name: ListFactoryOwners :many
SELECT * FROM factory_owners WHERE tenant_id=sqlc.arg(tenant_id) AND factory_id=sqlc.arg(factory_id)
 AND (sqlc.arg(include_inactive)::boolean OR status='ACTIVE') ORDER BY is_primary DESC,id;
-- name: CreateFactoryOwner :one
INSERT INTO factory_owners (tenant_id,factory_id,employee_id,employee_name,responsibility_code,is_primary,start_date,end_date,created_by,updated_by)
VALUES (sqlc.arg(tenant_id),sqlc.arg(factory_id),sqlc.arg(employee_id),sqlc.arg(employee_name),sqlc.arg(responsibility_code),sqlc.arg(is_primary),sqlc.narg(start_date),sqlc.narg(end_date),sqlc.arg(operator_id),sqlc.arg(operator_id)) RETURNING *;
-- name: UpdateFactoryOwner :one
UPDATE factory_owners SET employee_id=sqlc.arg(employee_id),employee_name=sqlc.arg(employee_name),responsibility_code=sqlc.arg(responsibility_code),is_primary=sqlc.arg(is_primary),start_date=sqlc.narg(start_date),end_date=sqlc.narg(end_date),updated_by=sqlc.arg(operator_id),updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND factory_id=sqlc.arg(factory_id) AND id=sqlc.arg(id) RETURNING *;
-- name: DeactivateFactoryOwner :execrows
UPDATE factory_owners SET status='INACTIVE',is_primary=false,updated_by=sqlc.arg(operator_id),updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND factory_id=sqlc.arg(factory_id) AND id=sqlc.arg(id) AND status='ACTIVE';
-- name: ClearFactoryPrimaryOwner :exec
UPDATE factory_owners SET is_primary=false,updated_by=sqlc.arg(operator_id),updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND factory_id=sqlc.arg(factory_id) AND status='ACTIVE' AND id<>sqlc.arg(except_id);

-- name: ListFactoryCapabilities :many
SELECT * FROM factory_capabilities WHERE tenant_id=sqlc.arg(tenant_id) AND factory_id=sqlc.arg(factory_id) ORDER BY id;
-- name: CreateFactoryCapability :one
INSERT INTO factory_capabilities (tenant_id,factory_id,product_category,process,monthly_capacity,capacity_unit,moq,lead_time_days,period_label,confirmed_on,remark,created_by,updated_by)
VALUES (sqlc.arg(tenant_id),sqlc.arg(factory_id),sqlc.arg(product_category),sqlc.arg(process),sqlc.arg(monthly_capacity),sqlc.arg(capacity_unit),sqlc.arg(moq),sqlc.arg(lead_time_days),sqlc.arg(period_label),sqlc.narg(confirmed_on),sqlc.arg(remark),sqlc.arg(operator_id),sqlc.arg(operator_id)) RETURNING *;
-- name: DeleteFactoryCapability :execrows
DELETE FROM factory_capabilities WHERE tenant_id=sqlc.arg(tenant_id) AND factory_id=sqlc.arg(factory_id) AND id=sqlc.arg(id);

-- name: ListFactoryCertificates :many
SELECT * FROM factory_certificates WHERE tenant_id=sqlc.arg(tenant_id) AND factory_id=sqlc.arg(factory_id) ORDER BY expires_on NULLS LAST,id;
-- name: CreateFactoryCertificate :one
INSERT INTO factory_certificates (tenant_id,factory_id,name,certificate_no,issued_on,expires_on,status,file_key,remark,created_by,updated_by)
VALUES (sqlc.arg(tenant_id),sqlc.arg(factory_id),sqlc.arg(name),sqlc.arg(certificate_no),sqlc.narg(issued_on),sqlc.narg(expires_on),sqlc.arg(status),sqlc.arg(file_key),sqlc.arg(remark),sqlc.arg(operator_id),sqlc.arg(operator_id)) RETURNING *;
-- name: DeleteFactoryCertificate :execrows
DELETE FROM factory_certificates WHERE tenant_id=sqlc.arg(tenant_id) AND factory_id=sqlc.arg(factory_id) AND id=sqlc.arg(id);

-- name: CreateFactoryChange :exec
INSERT INTO factory_change_logs (tenant_id,factory_id,action,section,summary,before_data,after_data,operator_id,operator_name)
VALUES (sqlc.arg(tenant_id),sqlc.arg(factory_id),sqlc.arg(action),sqlc.arg(section),sqlc.arg(summary),sqlc.arg(before_data),sqlc.arg(after_data),sqlc.arg(operator_id),sqlc.arg(operator_name));
-- name: ListFactoryChanges :many
SELECT * FROM factory_change_logs WHERE tenant_id=sqlc.arg(tenant_id) AND factory_id=sqlc.arg(factory_id)
ORDER BY created_at DESC,id DESC LIMIT 200;

-- name: DeactivateSupplier :execrows
UPDATE suppliers SET status = 'INACTIVE', updated_by = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND status = 'ACTIVE';

-- name: ListOptions :many
SELECT * FROM option_items
WHERE tenant_id = $1
  AND status = 'ACTIVE'
  AND (sqlc.arg(category)::text = '' OR category = sqlc.arg(category))
ORDER BY category, sort_order, id;

-- name: CreateOption :one
INSERT INTO option_items (tenant_id, category, code, label, sort_order)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetNumberRule :one
SELECT * FROM number_rules WHERE tenant_id = $1 AND biz_type = $2;

-- name: ListNumberRules :many
SELECT * FROM number_rules WHERE tenant_id = $1 ORDER BY biz_type;

-- name: NextSeq :one
-- The whole generator in one atomic statement: first caller in a period
-- inserts seq 1, every concurrent caller after that serializes on the row
-- lock and gets a distinct increment. No duplicates, ever; gaps are fine.
INSERT INTO number_sequences (tenant_id, biz_type, period_key, next_seq)
VALUES ($1, $2, $3, 1)
ON CONFLICT (tenant_id, biz_type, period_key)
DO UPDATE SET next_seq = number_sequences.next_seq + 1
RETURNING next_seq;

-- name: ActivateCustomer :execrows
UPDATE customers
SET status = 'ACTIVE',
    business_status = CASE WHEN business_status = 'INACTIVE' THEN 'PROSPECT' ELSE business_status END,
    updated_by = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND status = 'INACTIVE';

-- name: ActivateSupplier :execrows
UPDATE suppliers SET status = 'ACTIVE', updated_by = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND status = 'INACTIVE';

-- name: ListMailingContacts :many
-- The address book for the mail composer.
--
-- The OR is split into a UNION on purpose. Written as one predicate the three
-- ILIKE conditions span two tables, and Postgres cannot use an index for an
-- OR that crosses a join — it has to join first and filter every pair, which
-- is a sequential scan however many trigram indexes exist. Each UNION branch
-- touches one table, so each can use its own index.
--
-- Contacts without an email are left out rather than returned greyed: a
-- picker row you cannot pick is noise. Deactivated customers likewise.
WITH hits AS (
    -- Matches on the person: uses the contact trigram indexes.
    SELECT cc.id
    FROM customer_contacts cc
    WHERE cc.tenant_id = sqlc.arg(tenant_id)::bigint
      AND cc.email <> ''
      AND cc.status = 'ACTIVE'
      AND cc.email_permission = 'ALLOWED'
      AND (cc.name ILIKE '%' || sqlc.arg(keyword)::text || '%'
           OR cc.email ILIKE '%' || sqlc.arg(keyword)::text || '%')
    UNION
    -- Matches on the company: uses the customer trigram index, then joins.
    SELECT cc.id
    FROM customers c
    JOIN customer_contacts cc
      ON cc.customer_id = c.id AND cc.tenant_id = c.tenant_id
    WHERE c.tenant_id = sqlc.arg(tenant_id)::bigint
      AND cc.email <> ''
      AND cc.status = 'ACTIVE'
      AND cc.email_permission = 'ALLOWED'
      AND c.name ILIKE '%' || sqlc.arg(keyword)::text || '%'
)
SELECT
    cc.id           AS contact_id,
    cc.name,
    cc.title,
    cc.email,
    cc.is_primary,
    c.id            AS customer_id,
    c.name          AS customer_name,
    c.country,
    c.country_code,
    cc.language,
    cc.email_categories
FROM customer_contacts cc
JOIN customers c ON c.id = cc.customer_id AND c.tenant_id = cc.tenant_id
WHERE cc.tenant_id = sqlc.arg(tenant_id)::bigint
  AND c.status = 'ACTIVE'
  AND cc.email <> ''
  AND cc.status = 'ACTIVE'
  AND cc.email_permission = 'ALLOWED'
  -- An empty keyword lists the book; anything else must have matched above.
  AND (sqlc.arg(keyword)::text = '' OR cc.id IN (SELECT id FROM hits))
  AND (
      cardinality(sqlc.arg(customer_ids)::bigint[]) = 0
      OR c.id = ANY(sqlc.arg(customer_ids)::bigint[])
  )
ORDER BY c.name, cc.is_primary DESC, cc.sort_order, cc.id
LIMIT 500;


-- name: ListCustomerCountries :many
-- Which countries this company sells to, and how many people could be written
-- to in each.
--
-- Two counts, not one, because they answer different questions and a picker
-- that shows only the first invites the wrong expectation: "Brazil (12)" reads
-- as twelve emails, and if those twelve companies have nineteen contacts
-- between them the send is half as big again as the person thought.
--
-- Customers with no code are grouped under '' rather than dropped. They are
-- the ones somebody has to go and fix, and a list that hides them is a list
-- that never gets fixed.
SELECT
    c.country_code,
    count(DISTINCT c.id)::bigint  AS customer_count,
    count(cc.id)::bigint          AS contact_count,
    -- Customers that have anybody writable, which is exactly how many rows
    -- ContactsInCountry returns. Counting is_primary flags instead would be
    -- wrong and quietly so: "primary" is a box somebody has to have ticked,
    -- often nobody has, and that query falls back to the first contact. The
    -- chip would then promise one recipient and add two.
    count(DISTINCT c.id) FILTER (WHERE cc.id IS NOT NULL)::bigint AS one_each_count
FROM customers c
LEFT JOIN customer_contacts cc
    ON cc.customer_id = c.id AND cc.tenant_id = c.tenant_id AND cc.email <> ''
   AND cc.status = 'ACTIVE' AND cc.email_permission = 'ALLOWED'
WHERE c.tenant_id = sqlc.arg(tenant_id)::bigint
  AND (sqlc.arg(status)::text = 'ALL' OR c.status = 'ACTIVE')
GROUP BY c.country_code
ORDER BY count(DISTINCT c.id) DESC, c.country_code;

-- name: ContactsInCountry :many
-- Everybody writable in one country.
--
-- One person per customer rather than everyone at it. Both are real
-- intentions — a price update goes to the buyer, an invitation to a
-- trade fair goes to whoever might come — so the caller says which, and
-- neither is assumed.
--
-- "Primary" is a flag somebody has to have set, and often nobody has. So the
-- fallback is the first contact by the same order the address book shows, and
-- DISTINCT ON gives exactly one row per customer either way.
SELECT DISTINCT ON (c.id)
    cc.id           AS contact_id,
    cc.name,
    cc.title,
    cc.email,
    cc.is_primary,
    c.id            AS customer_id,
    c.name          AS customer_name,
    c.country,
    c.country_code,
    cc.language,
    cc.email_categories
FROM customers c
JOIN customer_contacts cc
    ON cc.customer_id = c.id AND cc.tenant_id = c.tenant_id
WHERE c.tenant_id = sqlc.arg(tenant_id)::bigint
  AND c.status = 'ACTIVE'
  AND c.country_code = sqlc.arg(country_code)::text
  AND cc.email <> ''
  AND cc.status = 'ACTIVE'
  AND cc.email_permission = 'ALLOWED'
ORDER BY c.id, cc.is_primary DESC, cc.sort_order, cc.id;

-- name: AllContactsInCountry :many
-- The same country, everybody at every customer in it.
--
-- A separate query rather than a flag inside the one above, because DISTINCT
-- ON is what makes that one return a single contact per customer and there is
-- no way to switch it off from a parameter. Two queries that each do one thing
-- beat one that changes shape.
SELECT
    cc.id           AS contact_id,
    cc.name,
    cc.title,
    cc.email,
    cc.is_primary,
    c.id            AS customer_id,
    c.name          AS customer_name,
    c.country,
    c.country_code,
    cc.language,
    cc.email_categories
FROM customers c
JOIN customer_contacts cc
    ON cc.customer_id = c.id AND cc.tenant_id = c.tenant_id
WHERE c.tenant_id = sqlc.arg(tenant_id)::bigint
  AND c.status = 'ACTIVE'
  AND c.country_code = sqlc.arg(country_code)::text
  AND cc.email <> ''
  AND cc.status = 'ACTIVE'
  AND cc.email_permission = 'ALLOWED'
ORDER BY c.name, cc.is_primary DESC, cc.sort_order, cc.id;

-- name: RecordCreditRating :one
-- 记一次评级（E3）。历史表是事实来源，主数据行上的当前评级是它的投影。
INSERT INTO credit_ratings (
    tenant_id, party_type, party_id, grade, previous_grade,
    basis, evidence, rated_by, rated_by_name
) VALUES (
    sqlc.arg(tenant_id)::bigint, sqlc.arg(party_type)::text, sqlc.arg(party_id)::bigint,
    sqlc.arg(grade)::text, sqlc.arg(previous_grade)::text,
    sqlc.arg(basis)::text, sqlc.arg(evidence)::jsonb,
    sqlc.arg(rated_by)::bigint, sqlc.arg(rated_by_name)::text
)
RETURNING id, rated_at;

-- name: ListCreditRatings :many
-- 一个客户或供应商的评级变更史，最近的在前。
--
-- 这张列表本身就是这套东西的看门人：上一次评级是什么时候、依据是什么，
-- 摆在眼前，谁也说不出「一直都是 B」这种话。
SELECT id, grade, previous_grade, basis, evidence,
       rated_by, rated_by_name, rated_at
FROM credit_ratings
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND party_type = sqlc.arg(party_type)::text
  AND party_id = sqlc.arg(party_id)::bigint
ORDER BY rated_at DESC, id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: SetCustomerCreditGrade :execrows
UPDATE customers SET credit_grade = sqlc.arg(grade)::text, credit_graded_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: SetSupplierCreditGrade :execrows
UPDATE suppliers SET credit_grade = sqlc.arg(grade)::text, credit_graded_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: GetCustomerCreditGrade :one
SELECT coalesce(credit_grade, '')::text AS credit_grade,
       coalesce(credit_graded_at::text, '')::text AS credit_graded_at
FROM customers
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: GetSupplierCreditGrade :one
SELECT coalesce(credit_grade, '')::text AS credit_grade,
       coalesce(credit_graded_at::text, '')::text AS credit_graded_at
FROM suppliers
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: InsertNumberRuleIfAbsent :execrows
-- 懒播种的落笔（见 numbering.go）。DO NOTHING 而不是 UPDATE：已有的规则可能
-- 是人改过的，默认值永远不覆盖人的决定。
INSERT INTO number_rules (tenant_id, biz_type, prefix, period, seq_len)
VALUES (sqlc.arg(tenant_id)::bigint, sqlc.arg(biz_type)::text,
        sqlc.arg(prefix)::text, sqlc.arg(period)::text, sqlc.arg(seq_len)::int)
ON CONFLICT (tenant_id, biz_type) DO NOTHING;

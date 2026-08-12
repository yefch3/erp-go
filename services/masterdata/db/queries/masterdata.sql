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
ORDER BY status, responsibility_code, id;

-- name: CountActiveCustomerOwners :one
SELECT count(*) FROM customer_owners
WHERE tenant_id = sqlc.arg(tenant_id) AND customer_id = sqlc.arg(customer_id)
  AND status = 'ACTIVE';

-- name: CreateCustomerOwner :one
INSERT INTO customer_owners (
    tenant_id, customer_id, employee_id, employee_name, responsibility_code,
    start_date, end_date, created_by, updated_by
) VALUES (
    sqlc.arg(tenant_id), sqlc.arg(customer_id), sqlc.arg(employee_id), sqlc.arg(employee_name),
    sqlc.arg(responsibility_code), sqlc.narg(start_date), sqlc.narg(end_date),
    sqlc.arg(operator_id), sqlc.arg(operator_id)
)
RETURNING *;

-- name: DeactivateCustomerOwner :execrows
UPDATE customer_owners
SET status = 'INACTIVE', end_date = COALESCE(sqlc.narg(end_date), end_date, CURRENT_DATE),
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
SELECT id, code, name, tax_id
FROM customers
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (lower(name) = lower(sqlc.arg(name)) OR (sqlc.arg(tax_id)::text <> '' AND tax_id = sqlc.arg(tax_id)))
  AND (sqlc.arg(exclude_id)::bigint = 0 OR id <> sqlc.arg(exclude_id))
ORDER BY id
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
INSERT INTO suppliers (tenant_id, code, name, country, address, currency, contact_name, contact_phone, contact_email, remark, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
RETURNING *;

-- name: GetSupplier :one
SELECT * FROM suppliers WHERE tenant_id = $1 AND id = $2;

-- name: ListSuppliers :many
SELECT *, count(*) OVER () AS total
FROM suppliers
WHERE tenant_id = $1
  AND (sqlc.arg(status)::text = 'ALL' OR status = 'ACTIVE')
  AND (sqlc.arg(keyword)::text = '' OR name ILIKE '%' || sqlc.arg(keyword) || '%' OR code ILIKE '%' || sqlc.arg(keyword) || '%')
ORDER BY id DESC
LIMIT $2 OFFSET $3;

-- name: UpdateSupplier :one
UPDATE suppliers
SET name = $3, country = $4, address = $5, currency = $6,
    contact_name = $7, contact_phone = $8, contact_email = $9, remark = $10,
    updated_by = $11, updated_at = now()
WHERE tenant_id = $1 AND id = $2
RETURNING *;

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

-- name: CreateCustomer :one
-- country_code is the one that matters now; country keeps whatever free text
-- a caller still sends, and drains to empty as rows are edited through the
-- dropdown. See migration 00007 for why the code and not the name.
INSERT INTO customers (tenant_id, code, name, country, country_code, address, currency, payment_term, remark, created_by, updated_by)
VALUES ($1, $2, $3, $4, sqlc.arg(country_code)::text, $5, $6, $7, $8, $9, $9)
RETURNING *;

-- name: GetCustomer :one
SELECT * FROM customers WHERE tenant_id = $1 AND id = $2;

-- name: ListCustomers :many
SELECT *, count(*) OVER () AS total
FROM customers
WHERE tenant_id = $1
  AND (sqlc.arg(status)::text = 'ALL' OR status = 'ACTIVE')
  AND (sqlc.arg(keyword)::text = '' OR name ILIKE '%' || sqlc.arg(keyword) || '%' OR code ILIKE '%' || sqlc.arg(keyword) || '%')
ORDER BY id DESC
LIMIT $2 OFFSET $3;

-- name: UpdateCustomer :one
UPDATE customers
SET name = $3, country = $4, country_code = sqlc.arg(country_code)::text,
    address = $5, currency = $6,
    payment_term = $7, remark = $8, updated_by = $9, updated_at = now()
WHERE tenant_id = $1 AND id = $2
RETURNING *;

-- name: DeactivateCustomer :execrows
UPDATE customers SET status = 'INACTIVE', updated_by = $3, updated_at = now()
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
UPDATE customers SET status = 'ACTIVE', updated_by = $3, updated_at = now()
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
    c.country_code
FROM customer_contacts cc
JOIN customers c ON c.id = cc.customer_id AND c.tenant_id = cc.tenant_id
WHERE cc.tenant_id = sqlc.arg(tenant_id)::bigint
  AND c.status = 'ACTIVE'
  AND cc.email <> ''
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
WHERE c.tenant_id = sqlc.arg(tenant_id)::bigint
  AND c.status = 'ACTIVE'
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
    c.country_code
FROM customers c
JOIN customer_contacts cc
    ON cc.customer_id = c.id AND cc.tenant_id = c.tenant_id
WHERE c.tenant_id = sqlc.arg(tenant_id)::bigint
  AND c.status = 'ACTIVE'
  AND c.country_code = sqlc.arg(country_code)::text
  AND cc.email <> ''
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
    c.country_code
FROM customers c
JOIN customer_contacts cc
    ON cc.customer_id = c.id AND cc.tenant_id = c.tenant_id
WHERE c.tenant_id = sqlc.arg(tenant_id)::bigint
  AND c.status = 'ACTIVE'
  AND c.country_code = sqlc.arg(country_code)::text
  AND cc.email <> ''
ORDER BY c.name, cc.is_primary DESC, cc.sort_order, cc.id;

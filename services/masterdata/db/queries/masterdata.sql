-- name: CreateCustomer :one
INSERT INTO customers (tenant_id, code, name, country, address, currency, payment_term, remark, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
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
SET name = $3, country = $4, address = $5, currency = $6,
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
-- The address book for the mail composer. Contacts without an email are left
-- out rather than returned greyed: a picker row you cannot pick is noise.
-- Deactivated customers are excluded for the same reason.
SELECT
    cc.id           AS contact_id,
    cc.name,
    cc.title,
    cc.email,
    cc.is_primary,
    c.id            AS customer_id,
    c.name          AS customer_name,
    c.country
FROM customer_contacts cc
JOIN customers c ON c.id = cc.customer_id AND c.tenant_id = cc.tenant_id
WHERE cc.tenant_id = $1
  AND c.status = 'ACTIVE'
  AND cc.email <> ''
  AND (
      sqlc.arg(keyword)::text = ''
      OR cc.name  ILIKE '%' || sqlc.arg(keyword)::text || '%'
      OR cc.email ILIKE '%' || sqlc.arg(keyword)::text || '%'
      OR c.name   ILIKE '%' || sqlc.arg(keyword)::text || '%'
  )
  AND (
      cardinality(sqlc.arg(customer_ids)::bigint[]) = 0
      OR c.id = ANY(sqlc.arg(customer_ids)::bigint[])
  )
ORDER BY c.name, cc.is_primary DESC, cc.sort_order, cc.id
LIMIT 500;

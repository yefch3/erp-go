-- name: CreateFactoryRFQ :one
INSERT INTO factory_rfqs (tenant_id, case_id, rfq_no, supplier_id, supplier_code, supplier_name,
 factory_id, factory_code, factory_name, contact_email, currency, response_due_at, created_by, created_by_name,
 inquiry_channel, contact_name, contact_value, contacted_at, inquiry_note, round_no)
VALUES (sqlc.arg(tenant_id), sqlc.arg(case_id),
 'RFQ-' || to_char(current_date,'YYYYMMDD') || '-' || lpad(nextval('factory_rfq_no_seq')::text,6,'0'),
 sqlc.arg(supplier_id), sqlc.arg(supplier_code), sqlc.arg(supplier_name),
 sqlc.arg(factory_id), sqlc.arg(factory_code), sqlc.arg(factory_name), sqlc.arg(contact_email),
 sqlc.arg(currency), nullif(sqlc.arg(response_due_at)::text,'')::date,
 sqlc.arg(created_by), sqlc.arg(created_by_name), sqlc.arg(inquiry_channel), sqlc.arg(contact_name),
 sqlc.arg(contact_value), nullif(sqlc.arg(contacted_at)::text,'')::timestamptz, sqlc.arg(inquiry_note), sqlc.arg(round_no))
RETURNING id, rfq_no;

-- name: CreateFactoryRFQLine :exec
INSERT INTO factory_rfq_lines (tenant_id, factory_rfq_id, sourcing_line_id, line_no, qty, uom_code, spec_snapshot)
SELECT sqlc.arg(tenant_id), sqlc.arg(factory_rfq_id), sl.id, sl.line_no, sl.quantity, sl.quantity_unit,
 concat_ws(' / ', nullif(sl.material_standard,''), nullif(sl.grade,''), nullif(sl.thickness,''), nullif(sl.width,''), nullif(sl.length_or_form,''))
FROM sourcing_lines sl
WHERE sl.tenant_id=sqlc.arg(tenant_id) AND sl.case_id=sqlc.arg(case_id) AND sl.id=sqlc.arg(sourcing_line_id)
  AND sl.quantity IS NOT NULL AND sl.quantity > 0;

-- name: ListFactoryRFQs :many
SELECT r.id,r.case_id,r.rfq_no,r.supplier_id,r.supplier_code,r.supplier_name,
 r.factory_id,r.factory_code,r.factory_name,r.contact_email,
 r.currency,coalesce(r.response_due_at::text,'')::text AS response_due_at,r.status,r.created_at,
 r.inquiry_channel,r.contact_name,r.contact_value,coalesce(r.contacted_at::text,'')::text AS contacted_at,r.inquiry_note,r.round_no,
 (SELECT count(*)::int FROM factory_rfq_lines fl WHERE fl.tenant_id=r.tenant_id AND fl.factory_rfq_id=r.id) AS line_count,
 ARRAY(SELECT fl.sourcing_line_id FROM factory_rfq_lines fl WHERE fl.tenant_id=r.tenant_id AND fl.factory_rfq_id=r.id ORDER BY fl.line_no)::bigint[] AS sourcing_line_ids
FROM factory_rfqs r
WHERE r.tenant_id=$1 AND r.case_id=$2
ORDER BY r.created_at DESC;

-- name: UpdateFactoryRFQ :execrows
UPDATE factory_rfqs SET inquiry_channel=sqlc.arg(inquiry_channel),
 contact_name=sqlc.arg(contact_name),contact_email=sqlc.arg(contact_email),
 contact_value=sqlc.arg(contact_value),
 contacted_at=nullif(sqlc.arg(contacted_at)::text,'')::timestamptz,
 inquiry_note=sqlc.arg(inquiry_note),
 response_due_at=nullif(sqlc.arg(response_due_at)::text,'')::date,updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id)
  AND status IN ('DRAFT','SENT','PARTIALLY_QUOTED');

-- name: FactoryRFQForQuote :one
SELECT id,case_id,currency,status FROM factory_rfqs WHERE tenant_id=$1 AND id=$2 FOR UPDATE;

-- name: FactoryRFQCase :one
SELECT case_id FROM factory_rfqs WHERE tenant_id=$1 AND id=$2;

-- name: FactoryRFQLines :many
SELECT sourcing_line_id,qty::text,uom_code,spec_snapshot FROM factory_rfq_lines
WHERE tenant_id=$1 AND factory_rfq_id=$2 ORDER BY line_no;

-- name: GetFactoryRFQDocument :one
SELECT id,rfq_no,supplier_name,contact_email,currency,
 coalesce(response_due_at::text,'')::text AS response_due_at
FROM factory_rfqs WHERE tenant_id=$1 AND id=$2;

-- name: CreateSupplierQuote :one
INSERT INTO supplier_quotes (tenant_id,factory_rfq_id,supplier_quote_no,quoted_at,valid_until,currency,
 payment_terms,delivery,remark,source,created_by,version_no,confirmation_status,evidence_note)
VALUES (sqlc.arg(tenant_id),sqlc.arg(factory_rfq_id),
 'SQ-' || to_char(current_date,'YYYYMMDD') || '-' || lpad(nextval('supplier_quote_no_seq')::text,6,'0'),
 nullif(sqlc.arg(quoted_at)::text,'')::date,nullif(sqlc.arg(valid_until)::text,'')::date,
 sqlc.arg(currency),sqlc.arg(payment_terms),sqlc.arg(delivery),sqlc.arg(remark),sqlc.arg(source),sqlc.arg(created_by),
 (SELECT coalesce(max(version_no),0)+1 FROM supplier_quotes WHERE tenant_id=sqlc.arg(tenant_id) AND factory_rfq_id=sqlc.arg(factory_rfq_id)),
 sqlc.arg(confirmation_status),sqlc.arg(evidence_note))
RETURNING id,supplier_quote_no,version_no;

-- name: CreateSupplierQuoteLine :exec
INSERT INTO supplier_quote_lines (tenant_id,supplier_quote_id,sourcing_line_id,qty,unit_price,amount,moq,lead_time,remark)
VALUES (sqlc.arg(tenant_id),sqlc.arg(supplier_quote_id),sqlc.arg(sourcing_line_id),sqlc.arg(qty)::text::numeric,
 sqlc.arg(unit_price)::text::numeric,(sqlc.arg(qty)::text::numeric*sqlc.arg(unit_price)::text::numeric)::numeric(18,2),
 nullif(sqlc.arg(moq)::text,'')::numeric,sqlc.arg(lead_time),sqlc.arg(remark));

-- name: MarkFactoryRFQQuoted :exec
UPDATE factory_rfqs SET status='QUOTED',updated_at=now() WHERE tenant_id=$1 AND id=$2;

-- name: MarkFactoryRFQSent :execrows
UPDATE factory_rfqs SET status='SENT',updated_at=now()
WHERE tenant_id=$1 AND id=$2 AND status='DRAFT';

-- name: MarkSourcingCaseQuotesReceived :exec
UPDATE sourcing_cases SET status='QUOTES_RECEIVED',updated_at=now() WHERE tenant_id=$1 AND id=$2;

-- name: MarkSourcingCaseSourcing :exec
UPDATE sourcing_cases SET status='SOURCING',updated_at=now()
WHERE tenant_id=$1 AND id=$2 AND status='REVIEWING';

-- name: ListSupplierQuoteComparison :many
SELECT q.id AS quote_id,l.id AS quote_line_id,q.supplier_quote_no,q.factory_rfq_id,r.supplier_id,r.supplier_name,q.currency,
 coalesce(q.quoted_at::text,'')::text AS quoted_at,coalesce(q.valid_until::text,'')::text AS valid_until,
 q.payment_terms,q.delivery,q.remark,q.source,q.version_no,q.confirmation_status,q.evidence_note,
 l.sourcing_line_id,l.qty::text,l.unit_price::text,
 l.amount::text,coalesce(l.moq::text,'')::text AS moq,l.lead_time,l.remark AS line_remark
FROM supplier_quotes q JOIN factory_rfqs r ON r.id=q.factory_rfq_id AND r.tenant_id=q.tenant_id
JOIN supplier_quote_lines l ON l.supplier_quote_id=q.id AND l.tenant_id=q.tenant_id
WHERE q.tenant_id=$1 AND r.case_id=$2
ORDER BY l.sourcing_line_id,l.unit_price,q.created_at;

-- name: ListOverdueFactoryRFQs :many
-- 工作台的今日重点（B4）：过了回复期限还没报价的询价。围栏沿用询价
-- 案件的属主可见性——工作台只是列表的另一个入口，不是另一套权限。
SELECT r.id, r.rfq_no, r.case_id, c.case_no, r.supplier_name,
       r.response_due_at::text AS response_due_at,
       (current_date - r.response_due_at)::int AS overdue_days
FROM factory_rfqs r
JOIN sourcing_cases c ON c.id = r.case_id AND c.tenant_id = r.tenant_id
WHERE r.tenant_id = sqlc.arg(tenant_id)::bigint
  AND r.status IN ('SENT', 'PARTIALLY_QUOTED')
  AND r.response_due_at < current_date
  AND (sqlc.arg(visible_all)::bool
       OR c.owner_id = ANY(sqlc.arg(visible_ids)::bigint[]))
ORDER BY r.response_due_at, r.id
LIMIT sqlc.arg(row_limit)::int;

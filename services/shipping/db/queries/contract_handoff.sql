-- name: UpsertContractShippingHandoff :exec
INSERT INTO contract_shipping_handoffs (
 tenant_id,contract_id,contract_no,contract_version_id,version_no,customer_id,customer_name,
 batch_no,shipment_group_key,carrier_forwarder,service_option_name,customer_managed,currency,
 freight_amount,charge_basis,port_of_loading,port_of_discharge,estimated_departure,estimated_arrival,
 valid_until,remark,status
) VALUES (
 sqlc.arg(tenant_id),sqlc.arg(contract_id),sqlc.arg(contract_no),sqlc.arg(contract_version_id),sqlc.arg(version_no),
 sqlc.arg(customer_id),sqlc.arg(customer_name),sqlc.arg(batch_no),sqlc.arg(shipment_group_key),
 sqlc.arg(carrier_forwarder),sqlc.arg(service_option_name),sqlc.arg(customer_managed),sqlc.arg(currency),
 sqlc.arg(freight_amount)::text::numeric,sqlc.arg(charge_basis),sqlc.arg(port_of_loading),sqlc.arg(port_of_discharge),
 nullif(sqlc.arg(estimated_departure)::text,'')::date,nullif(sqlc.arg(estimated_arrival)::text,'')::date,
 nullif(sqlc.arg(valid_until)::text,'')::date,sqlc.arg(remark),
 CASE WHEN sqlc.arg(customer_managed)::bool THEN 'CUSTOMER_MANAGED'
      ELSE coalesce(nullif(sqlc.arg(initial_status)::text,''),'PENDING') END
)
ON CONFLICT (tenant_id,contract_version_id,batch_no) DO NOTHING;

-- name: SupersedeOldContractShippingHandoffs :exec
UPDATE contract_shipping_handoffs SET status='SUPERSEDED'
WHERE tenant_id=sqlc.arg(tenant_id) AND contract_id=sqlc.arg(contract_id)
  AND contract_version_id<>sqlc.arg(contract_version_id) AND status IN ('WAITING_REQUOTE','PENDING');

-- name: ListContractShippingHandoffs :many
SELECT id,contract_id,contract_no,contract_version_id,version_no,customer_id,customer_name,
 batch_no,shipment_group_key,carrier_forwarder,service_option_name,customer_managed,currency,
 freight_amount::text AS freight_amount,charge_basis,port_of_loading,port_of_discharge,
 coalesce(estimated_departure::text,'')::text AS estimated_departure,
 coalesce(estimated_arrival::text,'')::text AS estimated_arrival,
 coalesce(valid_until::text,'')::text AS valid_until,remark,status,coalesce(schedule_id,0)::bigint AS schedule_id,created_at
FROM contract_shipping_handoffs
WHERE tenant_id=sqlc.arg(tenant_id)
 AND (sqlc.arg(status)::text='' OR status=sqlc.arg(status)::text)
ORDER BY CASE status WHEN 'WAITING_REQUOTE' THEN 0 WHEN 'PENDING' THEN 1 ELSE 2 END,created_at DESC,id DESC;

-- name: GetContractShippingHandoffForUpdate :one
SELECT * FROM contract_shipping_handoffs WHERE tenant_id=$1 AND id=$2 FOR UPDATE;

-- name: MarkContractShippingHandoffScheduled :exec
UPDATE contract_shipping_handoffs SET status='SCHEDULED',schedule_id=sqlc.arg(schedule_id)
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND status='PAYMENT_REQUESTED';

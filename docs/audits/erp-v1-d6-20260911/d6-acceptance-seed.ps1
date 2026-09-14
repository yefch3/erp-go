$ErrorActionPreference = 'Stop'
$base = 'http://127.0.0.1:28282'
$password = 'D6-Acceptance-2026!'
$customerNameSql = "convert_from(decode('443620e9aa8ce694b6e5aea2e688b7','hex'),'UTF8')"
$forwarderNameSql = "convert_from(decode('443620e9aa8ce694b6e8b4a7e4bba3','hex'),'UTF8')"
$carrierNameSql = "convert_from(decode('443620e9aa8ce694b6e888b9e585ace58fb8','hex'),'UTF8')"

function Login([string]$email, [string]$secret) {
  $session = [Microsoft.PowerShell.Commands.WebRequestSession]::new()
  Invoke-RestMethod "$base/api/auth/login" -Method Post -ContentType 'application/json' -Body (@{ email = $email; password = $secret } | ConvertTo-Json) -WebSession $session | Out-Null
  return $session
}

function Api($session, [string]$method, [string]$path, $body) {
  $headers = @{}
  foreach ($cookie in $session.Cookies.GetCookies([uri]$base)) {
    if ($cookie.Name -eq 'erp_csrf') { $headers['X-CSRF-Token'] = $cookie.Value }
  }
  $args = @{ Uri = "$base/api$path"; Method = $method; WebSession = $session; Headers = $headers; ContentType = 'application/json' }
  if ($null -ne $body) { $args.Body = $body | ConvertTo-Json -Depth 20 -Compress }
  return (Invoke-RestMethod @args).data
}

$admin = Login 'admin@d5.example.test' 'admin123'
$roles = (Api $admin GET '/roles' $null).roles
$people = @{}
foreach ($pair in @(@('P1', 'BUYER'), @('L1', 'LOGISTICS'), @('F1', 'FINANCE'))) {
  $name = $pair[0]
  $email = "d6.$($name.ToLower())@d5.example.test"
  $code = "D6-$name"
  $employee = @((Api $admin GET "/employees?keyword=$code&size=100" $null).employees) | Where-Object code -eq $code | Select-Object -First 1
  if (!$employee) {
    $employee = (Api $admin POST '/employees' @{ code = $code; name = $name; email = $email; departmentId = '1'; initialPassword = $password; username = $email }).employee
  }
  $role = $roles | Where-Object code -eq $pair[1] | Select-Object -First 1
  if (!$role) { throw "Missing role $($pair[1])" }
  Api $admin POST "/employees/$($employee.id)/roles" @{ roleIds = @([string]$role.id) } | Out-Null
  docker exec erp-d6-20260911-postgres-1 psql -U erp_iam -d erp_iam -c "UPDATE employees SET email_verified_at=now() WHERE tenant_id=1 AND id=$($employee.id)" | Out-Null
  $people[$name] = @{ id = $employee.id; email = $email }
}

$customerSql = @"
INSERT INTO customers(tenant_id, code, name, country, currency, status, country_code, english_name, timezone, business_status)
VALUES(1, 'D6-CUST', $customerNameSql, 'Germany', 'USD', 'ACTIVE', 'DE', 'D6 Acceptance Customer', 'Europe/Berlin', 'COOPERATING')
ON CONFLICT (tenant_id, code) DO UPDATE SET status='ACTIVE', timezone=EXCLUDED.timezone
RETURNING id;
"@
docker exec erp-d6-20260911-postgres-1 psql -U erp_masterdata -d erp_masterdata -c $customerSql | Out-Null
$customerID = [int64](docker exec erp-d6-20260911-postgres-1 psql -U erp_masterdata -d erp_masterdata -Atc "SELECT id FROM customers WHERE tenant_id=1 AND code='D6-CUST'")
$supplierSql = @"
INSERT INTO suppliers(tenant_id, code, name, country, currency, status, name_zh, name_en, country_code, business_types)
VALUES
  (1, 'D6-FWD', $forwarderNameSql, 'China', 'USD', 'ACTIVE', $forwarderNameSql, 'D6 Acceptance Forwarder', 'CN', ARRAY['FORWARDER']),
  (1, 'D6-CARRIER', $carrierNameSql, 'Germany', 'USD', 'ACTIVE', $carrierNameSql, 'D6 Acceptance Carrier', 'DE', ARRAY['CARRIER'])
ON CONFLICT (tenant_id, code) DO UPDATE SET status='ACTIVE', business_types=EXCLUDED.business_types;
"@
docker exec erp-d6-20260911-postgres-1 psql -U erp_masterdata -d erp_masterdata -c $supplierSql | Out-Null
$forwarderID = [int64](docker exec erp-d6-20260911-postgres-1 psql -U erp_masterdata -d erp_masterdata -Atc "SELECT id FROM suppliers WHERE tenant_id=1 AND code='D6-FWD'")
$carrierID = [int64](docker exec erp-d6-20260911-postgres-1 psql -U erp_masterdata -d erp_masterdata -Atc "SELECT id FROM suppliers WHERE tenant_id=1 AND code='D6-CARRIER'")

$sql = @"
DO `$`$
DECLARE h bigint;
BEGIN
  SELECT id INTO h FROM contract_shipping_handoffs WHERE tenant_id=1 AND contract_no='CT-D6-ACCEPT-001' LIMIT 1;
  IF h IS NULL THEN
    INSERT INTO contract_shipping_handoffs(
      tenant_id, contract_id, contract_no, contract_version_id, version_no, customer_id, customer_name,
      batch_no, shipment_group_key, carrier_forwarder, service_option_name, currency, freight_amount,
      charge_basis, port_of_loading, port_of_discharge, estimated_departure, estimated_arrival, valid_until,
      remark, status, final_forwarder_id, final_forwarder_name, actual_carrier_id, actual_carrier_name,
      final_service_option, final_currency, final_freight_amount, final_etd, final_eta, payment_terms,
      forwarder_contract_no, operator_id, operator_name, payment_requested_at
    ) VALUES (
      1, 6001001, 'CT-D6-ACCEPT-001', 6001001, 1, $customerID, $customerNameSql,
      1, 'D6-ONE-TRIP', $forwarderNameSql, '整柜海运', 'USD', 1800,
      '一票', 'Shanghai', 'Hamburg', '2026-09-20', '2026-10-20', '2026-09-30',
      'D6 一份合同一次运输验收数据', 'PAYMENT_REQUESTED', $forwarderID, $forwarderNameSql, $carrierID, $carrierNameSql,
      '整柜海运', 'USD', 1800, '2026-09-20', '2026-10-20', 'T/T 30 days',
      'D6-FWD-001', $($people.L1.id), 'L1', now()
    ) RETURNING id INTO h;
  ELSE
    -- 验收脚本可重复执行。早期夹具曾使用固定客户 ID；主数据卷复用后真实
    -- ID 会变化，因此已有委托也必须刷新跨库引用和对应名称快照。
    UPDATE contract_shipping_handoffs SET
      customer_id = $customerID,
      customer_name = $customerNameSql,
      final_forwarder_id = $forwarderID,
      final_forwarder_name = $forwarderNameSql,
      actual_carrier_id = $carrierID,
      actual_carrier_name = $carrierNameSql,
      updated_at = now()
    WHERE tenant_id = 1 AND id = h;
  END IF;
  INSERT INTO contract_shipping_handoff_cargo(
    tenant_id, handoff_id, contract_item_id, line_no, product_code, product_name, specification, quantity, uom_code
  ) VALUES (1, h, 6001001, 1, 'COIL-D6', '冷轧卷', 'Q235 / 1.2×1250mm', 10, 'MT')
  ON CONFLICT (tenant_id, handoff_id, contract_item_id) DO NOTHING;
END `$`$;
"@
docker exec erp-d6-20260911-postgres-1 psql -U erp_shipping -d erp_shipping -c $sql | Out-Null
$portSql = @"
INSERT INTO ports(tenant_id, unlocode, name_zh, name_en, country_code, city, timezone, created_by_name, updated_by_name)
VALUES
  (1, 'CNSHA', '上海港', 'Shanghai', 'CN', 'Shanghai', 'Asia/Shanghai', 'D6 seed', 'D6 seed'),
  (1, 'DEHAM', '汉堡港', 'Hamburg', 'DE', 'Hamburg', 'Europe/Berlin', 'D6 seed', 'D6 seed')
ON CONFLICT (tenant_id, unlocode) DO UPDATE SET status='ACTIVE', timezone=EXCLUDED.timezone;
"@
docker exec erp-d6-20260911-postgres-1 psql -U erp_masterdata -d erp_masterdata -c $portSql | Out-Null
$handoffID = docker exec erp-d6-20260911-postgres-1 psql -U erp_shipping -d erp_shipping -Atc "SELECT id FROM contract_shipping_handoffs WHERE tenant_id=1 AND contract_no='CT-D6-ACCEPT-001' LIMIT 1"

$evidence = [ordered]@{
  frontend = 'http://127.0.0.1:25375'
  gateway = $base
  password = $password
  accounts = @{ buyer = $people.P1.email; logistics = $people.L1.email; finance = $people.F1.email }
  fixture = @{ contract = 'CT-D6-ACCEPT-001'; handoffId = [string]$handoffID }
}
$evidence | ConvertTo-Json -Depth 6 | Set-Content (Join-Path $PSScriptRoot 'accounts.json') -Encoding utf8
Write-Output 'D6 acceptance accounts and delegated logistics order ready.'

$ErrorActionPreference = 'Stop'
$base = 'http://127.0.0.1:28282'
$password = 'D6-Acceptance-2026!'

function Login([string]$email) {
  $session = [Microsoft.PowerShell.Commands.WebRequestSession]::new()
  Invoke-RestMethod "$base/api/auth/login" -Method Post -ContentType 'application/json' -Body (@{ email = $email; password = $password } | ConvertTo-Json) -WebSession $session | Out-Null
  return $session
}

function Api($session, [string]$method, [string]$path, $body) {
  $headers = @{}
  foreach ($cookie in $session.Cookies.GetCookies([uri]$base)) {
    if ($cookie.Name -eq 'erp_csrf') { $headers['X-CSRF-Token'] = $cookie.Value }
  }
  $args = @{ Uri = "$base/api$path"; Method = $method; WebSession = $session; Headers = $headers; ContentType = 'application/json' }
  if ($null -ne $body) { $args.Body = $body | ConvertTo-Json -Depth 30 -Compress }
  return (Invoke-RestMethod @args).data
}

function ExpectApiStatus($session, [string]$method, [string]$path, $body, [int]$expected) {
  try {
    Api $session $method $path $body | Out-Null
  } catch {
    $actual = [int]$_.Exception.Response.StatusCode
    if ($actual -eq $expected) { return $actual }
    throw "Expected HTTP $expected for $method $path, got $actual"
  }
  throw "Expected HTTP $expected for $method $path, request succeeded"
}

$logistics = Login 'd6.l1@d5.example.test'
$buyer = Login 'd6.p1@d5.example.test'
$finance = Login 'd6.f1@d5.example.test'
$logisticsID = [int64](docker exec erp-d6-20260911-postgres-1 psql -U erp_iam -d erp_iam -Atc "SELECT id FROM employees WHERE tenant_id=1 AND email='d6.l1@d5.example.test'")
$loadingPortID = [int64](docker exec erp-d6-20260911-postgres-1 psql -U erp_masterdata -d erp_masterdata -Atc "SELECT id FROM ports WHERE tenant_id=1 AND unlocode='CNSHA'")
$dischargePortID = [int64](docker exec erp-d6-20260911-postgres-1 psql -U erp_masterdata -d erp_masterdata -Atc "SELECT id FROM ports WHERE tenant_id=1 AND unlocode='DEHAM'")
$customerID = [int64](docker exec erp-d6-20260911-postgres-1 psql -U erp_masterdata -d erp_masterdata -Atc "SELECT id FROM customers WHERE tenant_id=1 AND code='D6-CUST'")
$forwarderID = [int64](docker exec erp-d6-20260911-postgres-1 psql -U erp_masterdata -d erp_masterdata -Atc "SELECT id FROM suppliers WHERE tenant_id=1 AND code='D6-FWD'")
$carrierID = [int64](docker exec erp-d6-20260911-postgres-1 psql -U erp_masterdata -d erp_masterdata -Atc "SELECT id FROM suppliers WHERE tenant_id=1 AND code='D6-CARRIER'")
$stamp = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
$contractID = 700000000 + $stamp
$contractNo = "CT-D6-SMOKE-$stamp"

$seedSql = @"
INSERT INTO contract_shipping_handoffs(
  tenant_id, contract_id, contract_no, contract_version_id, version_no, customer_id, customer_name,
  batch_no, shipment_group_key, carrier_forwarder, service_option_name, currency, freight_amount,
  charge_basis, port_of_loading, port_of_discharge, estimated_departure, estimated_arrival, valid_until,
  remark, status, final_forwarder_id, final_forwarder_name, actual_carrier_id, actual_carrier_name,
  final_service_option, final_currency, final_freight_amount, final_etd, final_eta, payment_terms,
  forwarder_contract_no, operator_id, operator_name, payment_requested_at
) VALUES (
  1, $contractID, '$contractNo', $contractID, 1, $customerID, 'D6 API 冒烟客户',
  1, 'ONE-TRIP', 'D6 冒烟货代', '整柜海运', 'USD', 2100,
  '一票', 'Shanghai', 'Hamburg', '2026-09-20', '2026-10-20', '2026-09-30',
  'D6 API smoke', 'PAYMENT_REQUESTED', $forwarderID, 'D6 验收货代', $carrierID, 'D6 验收船公司',
  '整柜海运', 'USD', 2100, '2026-09-20', '2026-10-20', 'T/T 30 days',
  'D6-SMOKE-FWD', $logisticsID, 'L1', now()
) RETURNING id;
"@
docker exec erp-d6-20260911-postgres-1 psql -U erp_shipping -d erp_shipping -c $seedSql | Out-Null
$handoffID = [int64](docker exec erp-d6-20260911-postgres-1 psql -U erp_shipping -d erp_shipping -Atc "SELECT id FROM contract_shipping_handoffs WHERE tenant_id=1 AND contract_no='$contractNo'")

$created = Api $logistics POST '/shipping/schedules' @{
  schedule = @{
    contractHandoffId = $handoffID; portOfLoading = 'Shanghai'; portOfDischarge = 'Hamburg'
    loadingPortId = $loadingPortID; loadingPortCode = 'CNSHA'; loadingPortTimezone = 'Asia/Shanghai'
    dischargePortId = $dischargePortID; dischargePortCode = 'DEHAM'; dischargePortTimezone = 'Europe/Berlin'
    etd = '2026-09-20'; eta = '2026-10-20'; warehouseEntryDate = '2026-09-14'; customsDeclarationDate = '2026-09-17'
    responsibleEmployeeId = $logisticsID; responsibleName = 'L1'; remark = 'D6 API smoke initial schedule'
  }
  confirmDuplicate = $true
}
$schedule = $created.schedule
if (!$schedule.id) { throw 'Schedule was not created' }
if ($schedule.freightCurrency -ne 'USD' -or [decimal]$schedule.freightAmount -ne 2100) { throw 'Freight snapshot did not carry from handoff' }
if ($schedule.status -ne 'PLANNED') { throw "Unexpected initial status $($schedule.status)" }

$duplicateStatus = 0
try {
  Api $logistics POST '/shipping/schedules' @{ schedule = @{ contractHandoffId = $handoffID; portOfLoading = 'Shanghai'; portOfDischarge = 'Hamburg'; loadingPortId = $loadingPortID; dischargePortId = $dischargePortID; etd = '2026-09-20'; eta = '2026-10-20'; responsibleEmployeeId = $logisticsID; responsibleName = 'L1' } } | Out-Null
} catch {
  $duplicateStatus = [int]$_.Exception.Response.StatusCode
}
if ($duplicateStatus -ne 409) { throw "Second schedule was not rejected with 409 (got $duplicateStatus)" }

$preference = (Api $logistics PUT '/shipping/reminder-preferences' @{ leadDays = @(14, 7, 3); timezone = 'Asia/Shanghai'; holidayCountryCodes = @() }).preference
if ($preference.timezone -ne 'Asia/Shanghai' -or $preference.leadDays.Count -ne 3 -or $preference.holidayCountryCodes.Count -ne 0) { throw 'Weekend-only reminder preference was not saved' }

$fileName = "d6-transport-$stamp.pdf"
$signed = Api $logistics POST "/shipping/schedules/$($schedule.id)/documents/presign" @{ fileName = $fileName }
$samplePDF = Join-Path (Resolve-Path (Join-Path $PSScriptRoot '../../..')) 'docs/audits/erp-v1-d2-20260907/evidence/D2-product-subtotal.pdf'
$payload = [IO.File]::ReadAllBytes($samplePDF)
Invoke-WebRequest $signed.uploadUrl -Method Put -Body $payload -ContentType 'application/pdf' -UseBasicParsing | Out-Null
$document = (Api $logistics POST "/shipping/schedules/$($schedule.id)/documents" @{ fileKey = $signed.fileKey; fileName = $fileName; category = 'OTHER'; remark = 'D6 transportation attachment' }).document
if (!$document.id) { throw 'Transportation attachment was not registered' }

$updated = (Api $logistics PUT "/shipping/schedules/$($schedule.id)" @{
  schedule = @{
    contractHandoffId = $handoffID; contractNo = $contractNo; customerId = $customerID; customerName = 'D6 API 冒烟客户'
    carrierId = $carrierID; carrierForwarder = 'D6 验收船公司'; vesselName = 'D6 OCEAN'; voyageNo = 'V001'
    portOfLoading = 'Shanghai'; portOfDischarge = 'Hamburg'; loadingPortId = $loadingPortID; loadingPortCode = 'CNSHA'; loadingPortTimezone = 'Asia/Shanghai'; dischargePortId = $dischargePortID; dischargePortCode = 'DEHAM'; dischargePortTimezone = 'Europe/Berlin'
    etd = '2026-09-22'; eta = '2026-10-18'; bookingNo = 'BOOK-D6-001'; billOfLadingNo = 'BL-D6-001'
    warehouseEntryDate = '2026-09-16'; customsDeclarationDate = '2026-09-18'
    freightCurrency = 'USD'; freightAmount = '2100'; responsibleEmployeeId = $logisticsID; responsibleName = 'L1'; remark = 'D6 API smoke updated schedule'
  }
  dateChangeReason = 'API smoke: ETD delayed and ETA advanced'
  confirmDuplicate = $true
}).schedule
if ($updated.bookingNo -ne 'BOOK-D6-001' -or $updated.warehouseEntryDate -ne '2026-09-16') { throw 'Execution fields were not updated' }

$buyerAlerts = (Api $buyer GET '/shipping/operational-alerts?open_only=true' $null).items
$buyerAlert = @($buyerAlerts | Where-Object scheduleId -eq $schedule.id | Select-Object -First 1)[0]
if (!$buyerAlert) { throw 'Buyer did not receive ETD/warehouse delay action' }
$reminderPage = Api $buyer GET '/home/reminders?source=SHIPPING_ACTION&page=1&page_size=100' $null
if (!(@($reminderPage.items | Where-Object sourceId -eq ([string]$buyerAlert.id)).Count)) { throw 'Buyer action did not appear in My Todos' }
Api $buyer POST "/shipping/operational-alerts/$($buyerAlert.id)/resolve" @{ resolutionNote = '已联系工厂并确认新的进仓安排' } | Out-Null

$financeAlerts = (Api $finance GET '/shipping/operational-alerts?open_only=true' $null).items
if (!(@($financeAlerts | Where-Object { $_.scheduleId -eq $schedule.id -and $_.alertType -eq 'ETA_ADVANCED' }).Count)) { throw 'Finance did not receive ETA advanced action' }
$financeSchedule = (Api $finance GET "/shipping/schedules/$($schedule.id)" $null).schedule
$financeDocuments = (Api $finance GET "/shipping/schedules/$($schedule.id)/documents" $null).documents
if ($financeSchedule.freightAmount -ne '2100' -or !(@($financeDocuments | Where-Object id -eq $document.id).Count)) { throw 'Finance cannot view freight or transportation attachment' }
$download = Api $finance POST "/shipping/schedules/$($schedule.id)/documents/$($document.id)/download" @{}
$downloaded = Invoke-WebRequest $download.url -UseBasicParsing
if ($downloaded.StatusCode -ne 200 -or $downloaded.RawContentLength -ne $payload.Length) { throw 'Finance attachment download did not preserve the uploaded file' }
$financeWriteDenied = ExpectApiStatus $finance PUT "/shipping/schedules/$($schedule.id)" @{} 403
$financeUploadDenied = ExpectApiStatus $finance POST "/shipping/schedules/$($schedule.id)/documents/presign" @{ fileName = 'forbidden.pdf' } 403
$buyerDocumentDenied = ExpectApiStatus $buyer GET "/shipping/schedules/$($schedule.id)/documents" $null 403

$evidence = [ordered]@{
  generatedAt = [DateTimeOffset]::Now.ToString('o')
  contractNo = $contractNo
  handoffId = [string]$handoffID
  scheduleId = [string]$schedule.id
  scheduleNo = $schedule.scheduleNo
  duplicateCreateStatus = $duplicateStatus
  freight = "$($financeSchedule.freightCurrency) $($financeSchedule.freightAmount)"
  executionFields = @{ bookingNo = $updated.bookingNo; billOfLadingNo = $updated.billOfLadingNo; warehouseEntryDate = $updated.warehouseEntryDate; customsDeclarationDate = $updated.customsDeclarationDate }
  reminderPreference = @{ leadDays = $preference.leadDays; timezone = $preference.timezone; weekendOnly = $true; countries = $preference.holidayCountryCodes }
  alerts = @{ buyer = @($buyerAlerts | Where-Object scheduleId -eq $schedule.id).Count; finance = @($financeAlerts | Where-Object scheduleId -eq $schedule.id).Count; buyerResolution = '已联系工厂并确认新的进仓安排' }
  attachment = @{ id = [string]$document.id; fileName = $document.fileName; financeVisible = $true; downloadedBytes = $downloaded.RawContentLength }
  permissions = @{ financeScheduleWrite = $financeWriteDenied; financeDocumentUpload = $financeUploadDenied; buyerDocumentView = $buyerDocumentDenied }
}
$evidence | ConvertTo-Json -Depth 8 | Set-Content (Join-Path $PSScriptRoot 'api-smoke-evidence.json') -Encoding utf8
Write-Output "D6 API smoke passed: schedule $($schedule.scheduleNo), weekend-only reminders, read-only finance access."

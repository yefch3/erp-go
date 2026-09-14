$ErrorActionPreference = 'Stop'
$base = 'http://127.0.0.1:28382'
$password = 'D5-Acceptance-2026!'

function Login([string]$email,[string]$loginPassword=$password) {
  Invoke-RestMethod "$base/api/auth/login" -Method Post -ContentType 'application/json' -Body (@{email=$email;password=$loginPassword}|ConvertTo-Json) -SessionVariable loginSession | Out-Null
  return $loginSession
}
function Request($session,[string]$method,[string]$path,$body) {
  $headers=@{}
  foreach($cookie in $session.Cookies.GetCookies([uri]$base)){if($cookie.Name -eq 'erp_csrf'){$headers['X-CSRF-Token']=$cookie.Value}}
  $args=@{Uri="$base/api$path";Method=$method;WebSession=$session;Headers=$headers;ContentType='application/json'}
  if($null-ne$body){$args.Body=$body|ConvertTo-Json -Depth 20 -Compress}
  return (Invoke-RestMethod @args).data
}
function ExpectStatus($session,[string]$method,[string]$path,$body,[int]$status) {
  try { Request $session $method $path $body | Out-Null; throw "Expected HTTP $status" }
  catch { if([int]$_.Exception.Response.StatusCode -ne $status){throw} }
}

$buyerId=(docker exec erp-gap-20260913-postgres-1 psql -U erp_iam -d erp_iam -At -c "SELECT id FROM employees WHERE tenant_id=1 AND code='D5-P1'").Trim()
$salesId=(docker exec erp-gap-20260913-postgres-1 psql -U erp_iam -d erp_iam -At -c "SELECT id FROM employees WHERE tenant_id=1 AND code='D5-S1'").Trim()
if(!$buyerId -or !$salesId){throw 'D5 acceptance accounts are missing'}
$sql="DO `$`$ DECLARE r bigint; p bigint; BEGIN IF NOT EXISTS(SELECT 1 FROM purchase_orders WHERE tenant_id=1 AND po_no='PO-GAP-SMOKE-001') THEN INSERT INTO purchase_requirements(tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_code,product_name,uom_id,uom_code,required_qty,ordered_qty,status,owner_id) VALUES(1,9701,'CT-GAP-SMOKE',1,9701,102,'PIPE','焊接钢管',1,'MT',10,10,'ORDERED',$salesId) RETURNING id INTO r; INSERT INTO purchase_orders(tenant_id,po_no,supplier_id,supplier_code,supplier_name,currency,total_amount,expected_date,status,buyer_id,buyer_name,ordered_at,business_type) VALUES(1,'PO-GAP-SMOKE-001',902,'GAP-F01','补齐冒烟工厂','CNY',20000,'2026-10-20','ORDERED',$buyerId,'P1',now(),'PROCUREMENT') RETURNING id INTO p; INSERT INTO purchase_order_items(tenant_id,po_id,requirement_id,product_id,product_code,product_name,spec,uom_id,uom_code,qty,unit_price,amount) VALUES(1,p,r,102,'PIPE','焊接钢管','Q235 / 60mm',1,'MT',10,2000,20000); END IF; END `$`$;"
docker exec erp-gap-20260913-postgres-1 psql -v ON_ERROR_STOP=1 -U erp_procurement -d erp_procurement -c $sql | Out-Null
$ids=(docker exec erp-gap-20260913-postgres-1 psql -U erp_procurement -d erp_procurement -At -F '|' -c "SELECT o.id,i.id FROM purchase_orders o JOIN purchase_order_items i ON i.po_id=o.id AND i.tenant_id=o.tenant_id WHERE o.tenant_id=1 AND o.po_no='PO-GAP-SMOKE-001'").Trim().Split('|')
$poId=$ids[0];$itemId=$ids[1]

$buyer=Login 'p1@d5.example.test'
$quality=Login 'q1@d5.example.test'
$otherBuyer=Login 'd6.p1@d5.example.test' 'D6-Acceptance-2026!'
$cleanup="BEGIN; DELETE FROM quality_inspection_files WHERE tenant_id=1 AND task_id IN (SELECT id FROM quality_inspection_tasks WHERE tenant_id=1 AND po_id=$poId); DELETE FROM quality_inspection_round_lines WHERE tenant_id=1 AND round_id IN (SELECT r.id FROM quality_inspection_rounds r JOIN quality_inspection_tasks t ON t.id=r.task_id AND t.tenant_id=r.tenant_id WHERE t.tenant_id=1 AND t.po_id=$poId); DELETE FROM quality_inspection_rounds WHERE tenant_id=1 AND task_id IN (SELECT id FROM quality_inspection_tasks WHERE tenant_id=1 AND po_id=$poId); DELETE FROM quality_inspection_task_lines WHERE tenant_id=1 AND task_id IN (SELECT id FROM quality_inspection_tasks WHERE tenant_id=1 AND po_id=$poId); DELETE FROM quality_inspection_tasks WHERE tenant_id=1 AND po_id=$poId; COMMIT;"
docker exec erp-gap-20260913-postgres-1 psql -v ON_ERROR_STOP=1 -U erp_procurement -d erp_procurement -c $cleanup | Out-Null

$task=(Request $buyer POST "/purchase-orders/$poId/quality-inspections" @{inspectionLocation='补齐冒烟工厂';lines=@(@{poItemId=$itemId;qty='10'})}).task
$taskId=$task.id
$qualityTask=(Request $quality GET "/quality/tasks/$taskId" $null).task
$lineId=$qualityTask.lines[0].id
$task=(Request $quality POST "/quality/tasks/$taskId/rounds" @{inspectionLocation='补齐冒烟工厂';lines=@(@{taskLineId=$lineId;result='PARTIAL';inspectedQty='10';qualifiedQty='8';unqualifiedQty='2';issueDescription='表面划痕';handlingSuggestion='返工复检'})}).task
if($task.procurementHandlingStatus -ne 'PENDING'){throw 'Procurement todo was not created'}

$presign=Request $quality POST "/quality/tasks/$taskId/files/presign" @{fileName='gap-quality-report.txt'}
$bytes=[Text.Encoding]::UTF8.GetBytes('gap completion quality attachment')
$uploadFile=Join-Path $env:TEMP 'erp-gap-quality-report.txt'
[IO.File]::WriteAllBytes($uploadFile,$bytes)
& curl.exe --silent --show-error --fail -X PUT -H 'Content-Type: text/plain' --upload-file $uploadFile $presign.uploadUrl
if($LASTEXITCODE){throw 'Quality attachment upload failed'}
$task=(Request $quality POST "/quality/tasks/$taskId/files" @{roundId=$task.rounds[0].id;taskLineId=$lineId;category='REPORT';fileKey=$presign.fileKey;fileName='gap-quality-report.txt';contentType='text/plain';sizeBytes=$bytes.Length}).task

$buyerTodos=@((Request $buyer GET '/quality/procurement-todos' $null).tasks)
if(!($buyerTodos|Where-Object id -eq $taskId)){throw 'PO owner did not receive the quality todo'}
$otherTodos=@((Request $otherBuyer GET '/quality/procurement-todos' $null).tasks)
if($otherTodos|Where-Object id -eq $taskId){throw 'Unrelated buyer received the quality todo'}
$orderQuality=@((Request $buyer GET "/purchase-orders/$poId/quality-inspections" $null).tasks)
if(!$orderQuality -or $orderQuality[0].lines[0].unresolvedQty -ne '2.0000' -or $orderQuality[0].files.Count -lt 1){throw 'Read-only PO quality summary is incomplete'}
ExpectStatus $buyer GET "/quality/tasks/$taskId" $null 403
ExpectStatus $buyer POST "/quality/tasks/$taskId/release" @{lines=@(@{taskLineId=$lineId;qty='8'})} 403

$handled=(Request $buyer POST "/purchase-orders/$poId/quality-inspections/handling" @{task_id=$taskId;action='REWORK';result_note='工厂返工后重新验货'}).task
if($handled.procurementHandlingStatus -ne 'COMPLETED'){throw 'Procurement handling was not saved'}
$buyerTodos=@((Request $buyer GET '/quality/procurement-todos' $null).tasks)
if($buyerTodos|Where-Object id -eq $taskId){throw 'Completed procurement todo remained pending'}
$completed=(Request $quality POST "/quality/tasks/$taskId/rounds" @{inspectionLocation='补齐冒烟工厂';lines=@(@{taskLineId=$lineId;result='PASS';inspectedQty='2';qualifiedQty='2';unqualifiedQty='0'})}).task
if($completed.status -ne 'COMPLETED' -or $completed.lines[0].approvedReleaseQty -ne '10.0000'){throw 'Reinspection did not complete the single-shipment release'}

$evidence=[ordered]@{
  testedAt=(Get-Date).ToString('o')
  purchaseOrder='PO-GAP-SMOKE-001'
  taskId=$taskId
  ownerTodo='created then completed'
  unrelatedBuyerTodo='not visible'
  purchaseOrderQuality='read-only conclusion, quantities, issue and attachment'
  qualityModuleForBuyer='HTTP 403'
  partialRelease='HTTP 403 and permission removed'
  finalStatus=$completed.status
  approvedForOnlyShipment=$completed.lines[0].approvedReleaseQty
}
$evidence|ConvertTo-Json -Depth 6|Set-Content (Join-Path $PSScriptRoot 'api-smoke-evidence.json') -Encoding utf8
Write-Output ($evidence|ConvertTo-Json -Compress)

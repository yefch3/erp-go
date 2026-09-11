$ErrorActionPreference = 'Stop'
$base = 'http://127.0.0.1:28282'
$password = 'D5-Acceptance-2026!'

function Login([string]$email) {
  $session = [Microsoft.PowerShell.Commands.WebRequestSession]::new()
  Invoke-RestMethod "$base/api/auth/login" -Method Post -ContentType 'application/json' -Body (@{email=$email;password=$password}|ConvertTo-Json) -WebSession $session | Out-Null
  return $session
}
function Request($session,[string]$method,[string]$path,$body) {
  $headers=@{}
  foreach($cookie in $session.Cookies.GetCookies([uri]$base)){if($cookie.Name -eq 'erp_csrf'){$headers['X-CSRF-Token']=$cookie.Value}}
  $args=@{Uri="$base/api$path";Method=$method;WebSession=$session;Headers=$headers;ContentType='application/json'}
  if($null-ne$body){$args.Body=$body|ConvertTo-Json -Depth 20 -Compress}
  return (Invoke-RestMethod @args).data
}
function ExpectHttpError($session,[string]$method,[string]$path,$body,[int]$status) {
  try { Request $session $method $path $body | Out-Null; throw "Expected HTTP $status" }
  catch { if([int]$_.Exception.Response.StatusCode -ne $status){throw} }
}

$buyerId=(docker exec erp-d5-20260910-postgres-1 psql -U erp_iam -d erp_iam -At -c "SELECT id FROM employees WHERE tenant_id=1 AND code='D5-P1'").Trim()
$salesId=(docker exec erp-d5-20260910-postgres-1 psql -U erp_iam -d erp_iam -At -c "SELECT id FROM employees WHERE tenant_id=1 AND code='D5-S1'").Trim()
$sql="DO `$`$ DECLARE r bigint; p bigint; BEGIN IF NOT EXISTS(SELECT 1 FROM purchase_orders WHERE tenant_id=1 AND po_no='PO-D5-SMOKE-001') THEN INSERT INTO purchase_requirements(tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_code,product_name,uom_id,uom_code,required_qty,ordered_qty,status,owner_id) VALUES(1,5101,'CT-D5-SMOKE',1,5101,102,'PIPE','焊接钢管',1,'MT',10,10,'ORDERED',$salesId) RETURNING id INTO r; INSERT INTO purchase_orders(tenant_id,po_no,supplier_id,supplier_code,supplier_name,currency,total_amount,expected_date,status,buyer_id,buyer_name,ordered_at,business_type) VALUES(1,'PO-D5-SMOKE-001',902,'D5-F02','D5 冒烟工厂','CNY',20000,'2026-10-20','ORDERED',$buyerId,'P1',now(),'PROCUREMENT') RETURNING id INTO p; INSERT INTO purchase_order_items(tenant_id,po_id,requirement_id,product_id,product_code,product_name,spec,uom_id,uom_code,qty,unit_price,amount) VALUES(1,p,r,102,'PIPE','焊接钢管','Q235 / 60mm',1,'MT',10,2000,20000); END IF; END `$`$;"
docker exec erp-d5-20260910-postgres-1 psql -U erp_procurement -d erp_procurement -c $sql | Out-Null
$ids=(docker exec erp-d5-20260910-postgres-1 psql -U erp_procurement -d erp_procurement -At -F '|' -c "SELECT o.id,i.id FROM purchase_orders o JOIN purchase_order_items i ON i.po_id=o.id AND i.tenant_id=o.tenant_id WHERE o.tenant_id=1 AND o.po_no='PO-D5-SMOKE-001'").Trim().Split('|')
$poId=$ids[0];$itemId=$ids[1]

$buyer=Login 'p1@d5.example.test';$quality=Login 'q1@d5.example.test';$logistics=Login 'l1@d5.example.test';$sales=Login 's1@d5.example.test';$boss=Login 'b1@d5.example.test'
$existing=@((Request $buyer GET '/quality/tasks?tab=PENDING&keyword=PO-D5-SMOKE-001' $null).tasks)
if($existing.Count -eq 0){$task=(Request $buyer POST "/purchase-orders/$poId/quality-inspections" @{expectedDate='2026-09-15T00:00:00Z';inspectionLocation='D5 冒烟工厂';contactName='QC contact';contactPhone='10086';remark='自动冒烟';lines=@(@{poItemId=$itemId;qty='6'})}).task}else{$task=$existing[0]}
$taskId=$task.id
if($task.status -eq 'WAITING'){$task=(Request $quality POST "/quality/tasks/$taskId/start" $null).task}
$task=(Request $quality GET "/quality/tasks/$taskId" $null).task
$lineId=$task.lines[0].id
if($task.rounds.Count -eq 0){$task=(Request $quality POST "/quality/tasks/$taskId/rounds" @{inspectedAt='2026-09-11T00:00:00Z';inspectionLocation='D5 冒烟工厂';remark='第一轮';lines=@(@{taskLineId=$lineId;result='PARTIAL';inspectedQty='6';qualifiedQty='4';unqualifiedQty='2';issueDescription='表面划痕';handlingSuggestion='返修后复检'})}).task}
$task=(Request $buyer POST "/quality/tasks/$taskId/release" @{lines=@(@{taskLineId=$lineId;qty='3'})}).task

$presign=Request $quality POST "/quality/tasks/$taskId/files/presign" @{fileName='d5-smoke-report.txt'}
$bytes=[Text.Encoding]::UTF8.GetBytes('D5 quality attachment persistence smoke')
Invoke-WebRequest $presign.uploadUrl -Method Put -Body $bytes -ContentType 'text/plain' | Out-Null
$task=(Request $quality POST "/quality/tasks/$taskId/files" @{roundId=$task.rounds[0].id;taskLineId=$lineId;category='REPORT';fileKey=$presign.fileKey;fileName='d5-smoke-report.txt';contentType='text/plain';sizeBytes=$bytes.Length}).task

$logisticsView=(Request $logistics GET "/quality/tasks/$taskId" $null).task
$salesView=(Request $sales GET "/quality/tasks/$taskId" $null).task
$bossView=(Request $boss GET "/quality/tasks/$taskId" $null).task
ExpectHttpError $logistics POST "/quality/tasks/$taskId/rounds" @{lines=@()} 403
ExpectHttpError $buyer POST "/purchase-orders/$poId/quality-inspections" @{lines=@(@{poItemId=$itemId;qty='5'})} 409

if($task.status -ne 'REINSPECTION' -or $task.lines[0].qualifiedQty -ne '4.0000' -or $task.files.Count -lt 1){throw 'Partial result or attachment did not persist'}
$task=(Request $quality POST "/quality/tasks/$taskId/rounds" @{inspectedAt='2026-09-11T00:00:00Z';inspectionLocation='D5 冒烟工厂';remark='复检';lines=@(@{taskLineId=$lineId;result='PASS';inspectedQty='2';qualifiedQty='2';unqualifiedQty='0'})}).task
if($task.status -ne 'COMPLETED' -or $task.rounds.Count -ne 2 -or $task.lines[0].qualifiedQty -ne '6.0000'){throw 'Reinspection did not complete'}

$evidence=[ordered]@{testedAt=(Get-Date).ToString('o');purchaseOrder='PO-D5-SMOKE-001';taskId=$taskId;finalStatus=$task.status;qualifiedQty=$task.lines[0].qualifiedQty;rounds=$task.rounds.Count;files=$task.files.Count;buyerReleaseBeforeCompletion='3.0000';quantityOverflow='HTTP 409';logisticsMutation='HTTP 403';readers=@{logistics=$logisticsView.taskNo;sales=$salesView.taskNo;boss=$bossView.taskNo}}
$evidence|ConvertTo-Json -Depth 6|Set-Content (Join-Path $PSScriptRoot 'api-smoke-evidence.json') -Encoding utf8
Write-Output ($evidence|ConvertTo-Json -Depth 6 -Compress)

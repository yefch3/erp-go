param([switch]$SeedOnly)
$ErrorActionPreference='Stop'
$root=([IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../../..'))).TrimEnd([char[]]'\/')
& (Join-Path $root 'scripts/d4-environment.ps1') -Action CheckRuntime
$base='http://127.0.0.1:28281'
$password='D4-Acceptance-2026!'

function Login([string]$email,[string]$secret){
  $session=[Microsoft.PowerShell.Commands.WebRequestSession]::new()
  Invoke-RestMethod "$base/api/auth/login" -Method Post -ContentType 'application/json' -Body (@{email=$email;password=$secret}|ConvertTo-Json) -WebSession $session|Out-Null
  return $session
}
function Api($session,[string]$method,[string]$path,$body){
  $headers=@{}
  foreach($cookie in $session.Cookies.GetCookies([uri]$base)){if($cookie.Name -eq 'erp_csrf'){$headers['X-CSRF-Token']=$cookie.Value}}
  $args=@{Uri="$base/api$path";Method=$method;WebSession=$session;Headers=$headers;ContentType='application/json'}
  if($null -ne $body){$args.Body=($body|ConvertTo-Json -Depth 50 -Compress)}
  return (Invoke-RestMethod @args).data
}
function Assert($value,[string]$message){if(!$value){throw $message};Write-Output "PASS $message"}
function WaitContract($session,$id,[string]$expected){
  for($i=0;$i -lt 40;$i++){$contract=Api $session GET "/contracts/$id" $null;if($contract.contract.status -eq $expected){return $contract};Start-Sleep -Milliseconds 500}
  throw "Contract $id did not become $expected"
}
function WaitRelease($session,$id){
  for($i=0;$i -lt 60;$i++){
    $requirements=@((Api $session GET "/requirements?contract_id=$id&page_size=100" $null).requirements)
    $handoffs=@((Api $session GET '/shipping/contract-handoffs?status=WAITING_REQUOTE' $null).handoffs|Where-Object {$_.contractId -eq [string]$id})
    if($requirements.Count -eq 3 -and $handoffs.Count -eq 1){return @{requirements=$requirements;handoffs=$handoffs}}
    Start-Sleep -Milliseconds 500
  }
  throw "Contract $id did not reach both D4 queues"
}

$admin=Login 'admin@d4.example.test' 'admin123'
$roles=(Api $admin GET '/roles' $null).roles
$people=@{}
foreach($pair in @(@('S1','SALES'),@('B1','BOSS'),@('F1','FINANCE'),@('P1','BUYER'),@('L1','LOGISTICS'),@('LM1','SHIPPING_MANAGER'))){
  $name=$pair[0];$email="$($name.ToLower())@d4.example.test"
  $employee=(Api $admin GET "/employees?keyword=$name&size=100" $null).employees|Where-Object {$_.code -eq "D4-$name"}|Select-Object -First 1
  if(!$employee){$employee=(Api $admin POST '/employees' @{code="D4-$name";name=$name;email=$email;departmentId='1';initialPassword=$password;username=$email}).employee}
  $role=$roles|Where-Object {$_.code -eq $pair[1]}|Select-Object -First 1
  if(!$role){throw "Missing preset role $($pair[1])"}
  Api $admin POST "/employees/$($employee.id)/roles" @{roleIds=@([string]$role.id)}|Out-Null
  docker exec erp-d4-20260909-postgres-1 psql -U erp_iam -d erp_iam -c "UPDATE employees SET email_verified_at=now() WHERE tenant_id=1 AND id=$($employee.id) AND email LIKE '%@d4.example.test'"|Out-Null
  $people[$name]=@{id=$employee.id;email=$email;session=(Login $email $password)}
}
# Buyers work from the shared execution pool. This is fixture configuration,
# scoped to the disposable D4 tenant rather than a product migration default.
docker exec erp-d4-20260909-postgres-1 psql -U erp_iam -d erp_iam -c "UPDATE role_data_scopes s SET scope_type='ALL' FROM roles r WHERE s.tenant_id=1 AND s.role_id=r.id AND r.code='BUYER' AND s.module='procurement_requirement'"|Out-Null
# The disposable acceptance org has one explicit manager so procurement and
# procurement approvals are assigned to B1; logistics uses LM1 instead of the superadmin
# fallback used when a department has no leader.
$bossID=$people.B1.id
docker exec erp-d4-20260909-postgres-1 psql -U erp_iam -d erp_iam -c "UPDATE departments SET leader_employee_id=$bossID WHERE tenant_id=1 AND id=1"|Out-Null
docker exec erp-d4-20260909-postgres-1 psql -U erp_iam -d erp_iam -c "UPDATE employees SET manager_id=$bossID WHERE tenant_id=1 AND code IN ('D4-P1')"|Out-Null

$logisticsManagerID=$people.LM1.id
docker exec erp-d4-20260909-postgres-1 psql -U erp_iam -d erp_iam -c "UPDATE employees SET manager_id=$logisticsManagerID WHERE tenant_id=1 AND code='D4-L1'"|Out-Null
$customer=(Api $admin GET '/customers?keyword=D4%20验收客户&status=ALL&page_size=100' $null).customers|Where-Object {$_.code -eq 'D4-CUSTOMER'}|Select-Object -First 1
if(!$customer){
  $customer=(Api $admin POST '/customers' @{code='D4-CUSTOMER';name='D4 验收客户';country='China';countryCode='CN';currency='USD';paymentTerm='TT';address='D4 isolated data';contacts=@(@{name='验收联系人';email='customer@d4.example.test';phone='+86 13800000000';isPrimary=$true})}).customer
}
$contact=@($customer.contacts)|Where-Object {$_.isPrimary}|Select-Object -First 1
if(!$contact){$contact=@((Api $admin GET "/customers/$($customer.id)/contacts?status=ALL" $null).contacts)|Select-Object -First 1}
if(!$contact){throw 'D4 acceptance customer has no contact'}

$accountEvidence=[ordered]@{baseUrl='http://127.0.0.1:25374';gateway='http://127.0.0.1:28281';password=$password;accounts=@{sales=$people.S1.email;boss=$people.B1.email;finance=$people.F1.email;buyer=$people.P1.email;logistics=$people.L1.email;logisticsManager=$people.LM1.email}}
$evidenceDir=Join-Path $PSScriptRoot '../evidence';New-Item -ItemType Directory -Force $evidenceDir|Out-Null
$accountEvidence|ConvertTo-Json -Depth 5|Set-Content (Join-Path $evidenceDir 'accounts.json') -Encoding utf8
if($SeedOnly){Write-Output 'D4 accounts and customer ready';return}

function NewExecutingContract([string]$title){
  $s=$people.S1.session;$b=$people.B1.session
  $body=@{title=$title;customerId=[string]$customer.id;customer=$customer.name;contactId=[string]$contact.id;contact=$contact.name;delivery='2026-10-31';loadingPort='Shanghai';destinationPort='Hamburg';incoterm='FOB';remark='D4 isolated acceptance';products=@(
    @{product='冷轧卷';specification='Q235 / 1.2×1250mm';quantity='10';unit='MT'},
    @{product='热轧钢板';specification='Q355 / 6×1500×6000mm';quantity='5';unit='MT'},
    @{product='焊接钢管';specification='Q235 / 60mm';quantity='3';unit='MT'}
  )}
  $inquiry=(Api $s POST '/inquiry-workspace' @{action='save';view='SALES';body=$body}).item
  $inquiry=(Api $s POST '/inquiry-workspace' @{action='submit';view='SALES';id=$inquiry.id;revision=$inquiry.revision}).item
  $offer=Api $s POST '/customer-offer' @{action='get';caseId=$inquiry.id}
  $prices=@('110','126.50','145')
  for($i=0;$i -lt $offer.body.lines.Count;$i++){$offer.body.lines[$i].unitPrice=$prices[$i]}
  $offer=Api $s POST '/customer-offer' @{action='save';caseId=$inquiry.id;revision=$offer.revision;body=$offer.body}
  $offer=Api $s POST '/customer-offer' @{action='confirm';caseId=$inquiry.id;revision=$offer.revision}
  $id=$offer.contractId
  Api $s POST "/contracts/$id/submit" @{}|Out-Null
  $task=$null
  for($i=0;$i -lt 40 -and !$task;$i++){$task=@((Api $b GET '/approvals/todos?biz_type=CONTRACT&page_size=100' $null).todos)|Where-Object {$_.instance.bizId -eq [string]$id}|Select-Object -First 1;if(!$task){Start-Sleep -Milliseconds 500}}
  if(!$task){throw "Approval task missing for contract $id"}
  Api $b POST "/approvals/tasks/$($task.task.id)/act" @{action='APPROVE';comment='D4 acceptance approved'}|Out-Null
  $contract=WaitContract $s $id 'PENDING_SIGN'
  $upload=Api $s POST "/contracts/$id/files/presign" @{fileName='D4-signed-contract.pdf';contentType='application/pdf'}
  $pdf=[Text.Encoding]::ASCII.GetBytes("%PDF-1.4`n1 0 obj<</Type/Catalog>>endobj`n%%EOF")
  Invoke-WebRequest -Uri $upload.uploadUrl -Method Put -ContentType 'application/pdf' -Body $pdf|Out-Null
  Api $s POST "/contracts/$id/files" @{fileKey=$upload.fileKey;fileName='D4-signed-contract.pdf';kind='SIGNED';contractVersionId=$contract.version.id;contentType='application/pdf'}|Out-Null
  Api $s POST "/contracts/$id/sign" @{}|Out-Null
  WaitContract $s $id 'EXECUTING'|Out-Null
  return [string]$id
}

$automatedID=NewExecutingContract 'D4 自动回归合同'
$beforeReq=@((Api $admin GET "/requirements?contract_id=$automatedID&page_size=100" $null).requirements)
$beforeShip=@((Api $admin GET '/shipping/contract-handoffs' $null).handoffs|Where-Object {$_.contractId -eq $automatedID})
Assert ($beforeReq.Count -eq 0 -and $beforeShip.Count -eq 0) '财务确认前采购和物流没有正式执行任务'
$decision=Api $people.F1.session POST "/receivable-due/$automatedID/execution-condition" @{conditionType='NO_PREPAYMENT_REQUIRED';note='D4 自动回归'}
Assert ($decision.status -eq 'READY') '财务确认执行条件成功'
$released=WaitRelease $admin $automatedID
Assert (@($released.requirements|Where-Object {$_.status -ne 'WAITING_REQUOTE'}).Count -eq 0) '三项采购需求均停在待重新询价'
Assert ($released.handoffs[0].status -eq 'WAITING_REQUOTE' -and !$released.handoffs[0].carrierForwarder) '物流任务待重新询价且未锁定货代'
$again=Api $people.F1.session POST "/receivable-due/$automatedID/execution-condition" @{conditionType='SPECIAL_APPROVAL';note='不得覆盖'}
Assert ($again.conditionType -eq 'NO_PREPAYMENT_REQUIRED') '重复确认不覆盖首次财务决定'
$afterAgain=WaitRelease $admin $automatedID
Assert ($afterAgain.requirements.Count -eq 3 -and $afterAgain.handoffs.Count -eq 1) '重复确认不生成重复任务'

$manualID=NewExecutingContract 'D4 人工验收合同'
$result=[ordered]@{checkedAt=[DateTimeOffset]::Now.ToString('o');automatedContractId=$automatedID;manualContractId=$manualID;manualContractStatus='EXECUTING';productCount=3;preReleaseQueuesEmpty=$true;postReleaseProcurementStatus='WAITING_REQUOTE';postReleaseShippingStatus='WAITING_REQUOTE';finalSupplierLocked=$false;finalForwarderLocked=$false}
$result|ConvertTo-Json -Depth 5|Set-Content (Join-Path $evidenceDir 'd4-api-result.json') -Encoding utf8
Write-Output "D4 automated contract=$automatedID; manual acceptance contract=$manualID"

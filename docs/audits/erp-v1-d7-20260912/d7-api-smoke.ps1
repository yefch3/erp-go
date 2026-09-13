$ErrorActionPreference = 'Stop'
$base = 'http://127.0.0.1:28282'

function Login([string]$email, [string]$password) {
  $session = [Microsoft.PowerShell.Commands.WebRequestSession]::new()
  Invoke-RestMethod "$base/api/auth/login" -Method Post -ContentType 'application/json' `
    -Body (@{ email = $email; password = $password } | ConvertTo-Json) -WebSession $session | Out-Null
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

function ExpectStatus($session, [string]$method, [string]$path, $body, [int[]]$expected) {
  try { Api $session $method $path $body | Out-Null } catch {
    $actual = [int]$_.Exception.Response.StatusCode
    if ($expected -contains $actual) { return $actual }
    throw "Expected HTTP $($expected -join '/') for $method $path, got $actual"
  }
  throw "Expected failure for $method $path, request succeeded"
}

function WaitTask($session, [string]$claimID) {
  for ($i = 0; $i -lt 20; $i++) {
    $page = Api $session GET '/approvals/todos?biz_type=TRAVEL_REIMBURSEMENT&page=1&page_size=100' $null
    $match = @($page.todos | Where-Object { [string]$_.instance.bizId -eq $claimID } | Select-Object -First 1)
    if ($match.Count) { return $match[0] }
    Start-Sleep -Milliseconds 500
  }
  throw "Approval task for reimbursement $claimID did not arrive"
}

function WaitClaimStatus($session, [string]$claimID, [string]$status) {
  for ($i = 0; $i -lt 20; $i++) {
    $match = @((Api $session GET "/travel-reimbursements?status=$status" $null).items |
      Where-Object { [string]$_.id -eq $claimID } | Select-Object -First 1)
    if ($match.Count) { return $match[0] }
    Start-Sleep -Milliseconds 500
  }
  throw "Reimbursement $claimID did not enter $status"
}

$claimant = Login 'd6.l1@d5.example.test' 'D6-Acceptance-2026!'
$leader = Login 'b1@d5.example.test' 'D5-Acceptance-2026!'
$finance = Login 'd6.f1@d5.example.test' 'D6-Acceptance-2026!'
$unrelated = Login 'd6.p1@d5.example.test' 'D6-Acceptance-2026!'
$stamp = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()

$claim = (Api $claimant POST '/travel-reimbursements' @{
  tripStart = '2026-09-10'; tripEnd = '2026-09-12'; origin = '上海'; destination = '宁波'
  purpose = "D7 API 冒烟验收 $stamp"; amount = '680.50'; currency = 'CNY'
  paymentAccount = '招商银行工资卡'; note = '客户拜访交通与住宿'
}).reimbursement
if (!$claim.id -or $claim.status -ne 'DRAFT') { throw 'Travel reimbursement draft was not created' }
$claimID = [string]$claim.id

$missingDocument = ExpectStatus $claimant POST "/travel-reimbursements/$claimID/submit" @{} @(400,409)
$fileName = "d7-invoice-$stamp.pdf"
$signed = Api $claimant POST "/travel-reimbursements/$claimID/files/presign" @{ fileName = $fileName; category = 'INVOICE' }
$payload = [Text.Encoding]::UTF8.GetBytes("D7 reimbursement evidence $stamp")
Invoke-WebRequest $signed.uploadUrl -Method Put -Body $payload -ContentType 'application/pdf' -UseBasicParsing | Out-Null
$attached = (Api $claimant POST "/travel-reimbursements/$claimID/files" @{ objectKey = $signed.objectKey; fileName = $fileName; category = 'INVOICE' }).reimbursement
if ($attached.files.Count -lt 1) { throw 'Travel document was not registered' }

$submitted = (Api $claimant POST "/travel-reimbursements/$claimID/submit" @{}).reimbursement
if ($submitted.status -ne 'PENDING_DEPARTMENT_CONFIRMATION') { throw "Unexpected submit status $($submitted.status)" }

$leaderTask = WaitTask $leader $claimID
$selfDenied = ExpectStatus $claimant POST "/approvals/tasks/$($leaderTask.task.id)/act" @{ action = 'APPROVE'; comment = 'self approval must fail' } @(403)
Api $leader POST "/approvals/tasks/$($leaderTask.task.id)/act" @{ action = 'APPROVE'; comment = '部门负责人确认行程属实' } | Out-Null
$financeTask = WaitTask $finance $claimID
Api $finance POST "/approvals/tasks/$($financeTask.task.id)/act" @{ action = 'APPROVE'; comment = '财务负责人审批通过' } | Out-Null

$notVisible = @((Api $unrelated GET '/travel-reimbursements' $null).items | Where-Object id -eq $claimID).Count -eq 0
if (!$notVisible) { throw 'Unrelated employee can see another employee reimbursement' }
$payable = WaitClaimStatus $finance $claimID 'PENDING_PAYMENT'
$paid = (Api $finance POST "/travel-reimbursements/$claimID/pay" @{ paidAt = '2026-09-13'; paymentAccount = '公司基本户'; paymentReference = "D7-PAY-$stamp" }).reimbursement
if ($paid.status -ne 'PAID' -or $paid.history.Count -lt 6) { throw 'Payment or full history was not persisted' }

$fxStatus = (Api $finance GET '/fx/sync-status' $null).status
$watched = (Api $finance GET '/fx/watched' $null).currencies
$retiredBankWrite = ExpectStatus $finance POST '/bank-transactions' @{ amount = 1 } @(404,405)

$evidence = [ordered]@{
  generatedAt = [DateTimeOffset]::Now.ToString('o'); reimbursementId = $claimID; claimNo = $paid.claimNo
  amount = "$($paid.currency) $($paid.amount)"; status = $paid.status; historyEntries = $paid.history.Count
  attachment = @{ fileName = $fileName; uploadedBytes = $payload.Length }
  approval = @{ departmentLeader = 'b1@d5.example.test'; financeLeader = 'd6.f1@d5.example.test'; selfApprovalDenied = $selfDenied }
  visibility = @{ unrelatedEmployeeHidden = $notVisible }; missingDocumentStatus = $missingDocument
  retiredBankWriteStatus = $retiredBankWrite
  fx = @{ state = $fxStatus.state; provider = $fxStatus.provider; usingCache = $fxStatus.usingCache; watched = $watched }
}
$evidence | ConvertTo-Json -Depth 8 | Set-Content (Join-Path $PSScriptRoot 'api-smoke-evidence.json') -Encoding utf8
Write-Output "D7 API smoke passed: $($paid.claimNo), two-step approval, payment, attachment, visibility and FX status."

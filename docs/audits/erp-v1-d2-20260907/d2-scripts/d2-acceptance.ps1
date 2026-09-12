. (Join-Path $PSScriptRoot 'd1-regression.ps1') -SeedOnly
function Assert($value,[string]$message){if(!$value){throw $message};"PASS $message"}
function Denied($session,$method,$path,$body){try{Api $session $method $path $body|Out-Null}catch{if([int]$_.Exception.Response.StatusCode -in @(400,403,404,409)){return};throw};throw "Unexpected authorization: $path"}
function WaitContract($id,$expected){for($attempt=0;$attempt -lt 20;$attempt++){$c=Api $s GET "/contracts/$id" $null;if($c.contract.status -eq $expected){return $c};Start-Sleep -Milliseconds 500};throw "Contract did not become $expected"}
$s=$people.S1.session;$b=$people.B1.session
$products=@(1..101|ForEach-Object {@{product="D2 Steel $_";specification='Q235 2mm';quantity='100';unit='MT'}})
$inquiry=(Api $s POST '/inquiry-workspace' @{action='save';view='SALES';body=@{customer='D2 negotiated acceptance';contact='Customer';delivery='2026-10-01';products=$products}}).item
$inquiry=(Api $s POST '/inquiry-workspace' @{action='submit';view='SALES';id=$inquiry.id;revision=$inquiry.revision}).item
$offer=Api $s POST '/customer-offer' @{action='get';caseId=$inquiry.id}
Assert ($offer.body.lines.Count -eq 101) '101 original products received'
foreach($line in $offer.body.lines){$line.unitPrice='125';$line.quantity='60'}
$offer=Api $s POST '/customer-offer' @{action='save';caseId=$inquiry.id;revision=$offer.revision;body=$offer.body}
Assert ($offer.body.total -eq '757500.00') 'Negotiated 101 x 60 x 125 total'
Denied $people.P1.session POST '/customer-offer' @{action='get';caseId=$inquiry.id}
Denied $people.S2.session POST '/customer-offer' @{action='get';caseId=$inquiry.id}
$pdf=Api $s POST '/customer-offer' @{action='pdf';caseId=$inquiry.id}
[IO.File]::WriteAllBytes((Join-Path $PSScriptRoot '../evidence/D2-101-products.pdf'),[Convert]::FromBase64String($pdf.fileData))
$offer=Api $s POST '/customer-offer' @{action='confirm';caseId=$inquiry.id;revision=$offer.revision}
$id=$offer.contractId
$contract=Api $s GET "/contracts/$id" $null
Assert ($contract.items.Count -eq 101) 'Contract inherits 101 negotiated products'
Denied $people.P1.session GET "/contracts/$id" $null
Denied $s GET "/contracts/$id/receipts" $null
$approval=Api $s POST "/contracts/$id/submit" @{}
$tasks=(Api $b GET '/approvals/todos?biz_type=CONTRACT&page_size=100' $null).todos
$task=$tasks|Where-Object {$_.instance.bizId -eq $id}|Select-Object -First 1
Assert ($null -ne $task) 'Superior receives contract confirmation task'
Api $b POST "/approvals/tasks/$($task.task.id)/act" @{action='RETURN';comment='D2 test: supplement terms'}|Out-Null
WaitContract $id 'DRAFT'|Out-Null
Api $s POST "/contracts/$id/submit" @{}|Out-Null
$task=(Api $b GET '/approvals/todos?biz_type=CONTRACT&page_size=100' $null).todos|Where-Object {$_.instance.bizId -eq $id}|Select-Object -First 1
Api $b POST "/approvals/tasks/$($task.task.id)/act" @{action='APPROVE';comment='D2 test approved'}|Out-Null
$contract=WaitContract $id 'PENDING_SIGN'
Denied $s POST "/contracts/$id/sign" @{}
$upload=Api $s POST "/contracts/$id/files/presign" @{fileName='D2-test-signed.pdf';contentType='application/pdf'}
Invoke-WebRequest -Uri $upload.uploadUrl -Method Put -ContentType 'application/pdf' -Body ([Convert]::FromBase64String($pdf.fileData))|Out-Null
Api $s POST "/contracts/$id/files" @{fileKey=$upload.fileKey;fileName='D2-test-signed.pdf';kind='SIGNED';contractVersionId=$contract.version.id;contentType='application/pdf'}|Out-Null
Api $s POST "/contracts/$id/sign" @{}|Out-Null
WaitContract $id 'EXECUTING'|Out-Null
Assert $true 'Return, resubmit, one superior approval, signed upload, execution through real services'
@{inquiryId=$inquiry.id;contractId=$id;products=101;total='757500.00';status='EXECUTING';checkedAt=(Get-Date).ToUniversalTime().ToString('o')}|ConvertTo-Json|Set-Content (Join-Path $PSScriptRoot '../evidence/d2-api-result.json')

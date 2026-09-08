. (Join-Path $PSScriptRoot 'api-helpers.ps1')
function Assert($value,[string]$message){if(!$value){throw $message};"PASS $message"}
function Denied($session,$method,$path,$body){try{Api $session $method $path $body|Out-Null}catch{if([int]$_.Exception.Response.StatusCode -in @(400,403,404,409)){return};throw};throw "Unexpected authorization: $path"}
$s=Login 's1@d2.example.test' 'D2-Acceptance-2026!'
$b=Login 'b1@d2.example.test' 'D2-Acceptance-2026!'
$a=Login 'admin@d2.example.test' 'admin123'
$customer=(Api $a POST '/customers' @{code=('D2-H-'+[DateTimeOffset]::UtcNow.ToUnixTimeSeconds());name='D2 historical customer';currency='USD';countryCode='US';address='Isolated test address'}).customer
$body=@{customerId=$customer.id;currency='USD';signedDate='2026-08-01';effectiveDate='2026-08-02';openingReceivedAmount='50';externalContractNo=('D2-OFFLINE-'+[DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds());terms=@{deliveryDate='2026-10-01';portOfLoading='Manual port'};items=@(@{productName='Historical manual steel';uomCode='MT';qty='10';unitPrice='20';openingProcuredQty='6';openingArrivedQty='4';openingShippedQty='2'})}
Denied $s POST '/contracts/existing' $body
$upload=Api $s POST '/contracts/existing/files/presign' @{fileName='D2-historical-signed.pdf';contentType='application/pdf'}
$body.signedFileKey=$upload.fileKey;$body.signedFileName='D2-historical-signed.pdf'
Denied $s POST '/contracts/existing' $body
Invoke-WebRequest -Uri $upload.uploadUrl -Method Put -ContentType 'application/pdf' -InFile (Join-Path $PSScriptRoot '../evidence/D2-101-products.pdf')|Out-Null
$c=Api $s POST '/contracts/existing' $body
Assert ($c.contract.status -eq 'EXECUTING' -and !$c.contract.filePending) 'Historical import is executing with signed copy already recorded'
Assert ($c.items[0].productName -eq 'Historical manual steel' -and [decimal]$c.items[0].openingProcuredQty -eq 6 -and [decimal]$c.items[0].openingShippedQty -eq 2) 'Manual product and actual opening quantities retained without supplier or buyer gate'
$id=$c.contract.id
$read=Api $s GET "/contracts/$id" $null
Assert (!$read.contract.openingReceivedAmount) 'Sales detail hides opening receipt amount'
$finance=Api $a GET "/contracts/$id" $null
Assert ([decimal]$finance.contract.openingReceivedAmount -eq 50) 'Finance can read opening receipt amount'
$files=(Api $s GET "/contracts/$id/files" $null).files
Assert (@($files|Where-Object kind -eq 'SIGNED').Count -eq 1) 'Historical signed attachment stored atomically'
Denied $b POST "/contracts/$id/complete" @{}
Api $s POST "/contracts/$id/complete" @{}|Out-Null
$read=Api $s GET "/contracts/$id" $null
Assert ($read.contract.status -eq 'COMPLETED') 'Owner completes historical contract'
Denied $s POST "/contracts/$id/files/presign" @{fileName='late.pdf';contentType='application/pdf'}
@{contractId=$id;status='COMPLETED';signedFileCount=1;openingProcured=6;openingArrived=4;openingShipped=2;checkedAt=(Get-Date).ToUniversalTime().ToString('o')}|ConvertTo-Json|Set-Content (Join-Path $PSScriptRoot '../evidence/d2-historical.json')

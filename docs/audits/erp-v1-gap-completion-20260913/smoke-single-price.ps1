$ErrorActionPreference='Stop'
$base='http://127.0.0.1:28382';$password='D5-Acceptance-2026!'
function Login([string]$email){Invoke-RestMethod "$base/api/auth/login" -Method Post -ContentType application/json -Body (@{email=$email;password=$password}|ConvertTo-Json) -SessionVariable session|Out-Null;return $session}
function Request($session,[string]$path,$body,[bool]$idempotent=$false){$headers=@{};foreach($cookie in $session.Cookies.GetCookies([uri]$base)){if($cookie.Name-eq'erp_csrf'){$headers['X-CSRF-Token']=$cookie.Value}};if($idempotent){$headers['Idempotency-Key']=[guid]::NewGuid().ToString()};return (Invoke-RestMethod "$base/api$path" -Method Post -WebSession $session -Headers $headers -ContentType application/json -Body ($body|ConvertTo-Json -Depth 30 -Compress)).data}

$sales=Login 's1@d5.example.test'
$offer=Request $sales '/customer-offer' @{action='get';caseId='93';revision=0;body=@{}}
$original=$offer.body|ConvertTo-Json -Depth 40
$results=@()
foreach($formula in 1..5){
$b=$original|ConvertFrom-Json
$b.currency='USD';$b.quoteFx='7.05';$b.quoteFxConfirmed=$true;$b.incoterm=if($formula -eq 5){'FOB'}else{'CFR'}
$logistics=@($offer.source.quotes|Where-Object kind -eq 'LOGISTICS')[0]
$b|Add-Member -NotePropertyName logisticsQuoteId -NotePropertyValue $logistics.id -Force
$quantities=@{};foreach($line in $b.lines){$quantities[$line.id]=$line.quantity}
$b.transports=@(@{quoteId=$logistics.id;currency='CNY';price='210.50';title='上海远航货代';accepted=$true;quantities=$quantities})
$factory=@($offer.source.quotes|Where-Object kind -eq 'PROCUREMENT')[0]
foreach($line in $b.lines){$line.factoryQuoteId=$factory.id;$line.calculation.formula=$formula;$line.calculation.mtPerUnit='1'}
$r=Request $sales '/customer-offer' @{action='calculate_all';caseId='93';revision=$offer.revision;body=$b}
$expected=@('115.00','114.29','130.00','135.00','125.00')[$formula-1]
foreach($line in $r.body.lines){if($line.unitPrice -ne $expected){throw "Formula $formula got $($line.unitPrice) expected $expected"}}
$results+=@{formula=$formula;price=$r.body.lines[0].unitPrice;allocations=$r.body.logisticsAllocations}
}
$results|ConvertTo-Json -Depth 20|Set-Content (Join-Path $PSScriptRoot 'single-price-smoke.json') -Encoding utf8
$results|ConvertTo-Json -Depth 6

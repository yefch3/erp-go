$ErrorActionPreference='Stop'
$base='http://127.0.0.1:28382'
Invoke-RestMethod "$base/api/auth/login" -Method Post -ContentType application/json -Body (@{email='s1@d5.example.test';password='D5-Acceptance-2026!'}|ConvertTo-Json) -SessionVariable session | Out-Null
function Request($body){
 $headers=@{};foreach($cookie in $session.Cookies.GetCookies([uri]$base)){if($cookie.Name-eq'erp_csrf'){$headers['X-CSRF-Token']=$cookie.Value}}
 return (Invoke-RestMethod "$base/api/customer-offer" -Method Post -WebSession $session -Headers $headers -ContentType application/json -Body ($body|ConvertTo-Json -Depth 40 -Compress)).data
}
$before=Request @{action='get';caseId='93';body=@{}}
$original=$before.body|ConvertTo-Json -Depth 40 -Compress
$b=$original|ConvertFrom-Json
$b.incoterm='CFR';$b.quoteFx='7.05';$b.quoteFxConfirmed=$true
$logistics=@($before.source.quotes|Where-Object kind -eq 'LOGISTICS')[0]
$factory=@($before.source.quotes|Where-Object kind -eq 'PROCUREMENT')[0]
$b|Add-Member -NotePropertyName logisticsQuoteId -NotePropertyValue $logistics.id -Force
$quantities=@{};foreach($line in $b.lines){$quantities[$line.id]=$line.quantity;$line.calculation.formula=1;$line.calculation.mtPerUnit='1';$line.factoryQuoteId=$factory.id}
$b.transports=@(@{quoteId=$logistics.id;title='验收物流';currency='CNY';price='210.50';accepted=$true;quantities=$quantities})
$priced=Request @{action='calculate_all';caseId='93';revision=$before.revision;body=$b}
if($priced.pricingStale -or !$priced.body.pricingSnapshot -or $priced.body.total -ne '345.00'){throw 'CFR calculation/snapshot failed'}
$priced.body.quoteFx='6.72'
$rejected=$false
try {Request @{action='save';caseId='93';revision=$before.revision;body=$priced.body}|Out-Null} catch {if($_.ErrorDetails.Message -match 'OFFER_PRICING_STALE'){$rejected=$true}else{throw}}
if(!$rejected){throw 'Stale price save was not rejected'}
$b.incoterm='FOB';$b.transports=@();foreach($line in $b.lines){$line.calculation.formula=5}
$fob=Request @{action='calculate_all';caseId='93';revision=$before.revision;body=$b}
if($fob.pricingStale -or $fob.body.total -ne '375.00'){throw 'FOB without customer transport failed'}
$after=Request @{action='get';caseId='93';body=@{}}
if($after.revision -ne $before.revision -or ($after.body|ConvertTo-Json -Depth 40 -Compress) -ne $original){throw 'Manual acceptance data was modified'}
@{caseId='93';oldPriceRequiresReview=$before.pricingStale;CFRTotal=$priced.body.total;FOBTotal=$fob.body.total;staleSaveRejected=$rejected;manualDataPreserved=$true}|ConvertTo-Json | Tee-Object -FilePath (Join-Path $PSScriptRoot 'quote-selection-smoke.json')

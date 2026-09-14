param([string]$FixtureTitle = ('报价流程自动回归-'+(Get-Date -Format 'yyyyMMdd-HHmmss')))
$ErrorActionPreference='Stop'
$base='http://127.0.0.1:28382';$password='D5-Acceptance-2026!'
function Login([string]$email){Invoke-RestMethod "$base/api/auth/login" -Method Post -ContentType application/json -Body (@{email=$email;password=$password}|ConvertTo-Json) -SessionVariable session|Out-Null;return $session}
function Request($session,[string]$path,$body,[bool]$idempotent=$false){$headers=@{};foreach($cookie in $session.Cookies.GetCookies([uri]$base)){if($cookie.Name-eq'erp_csrf'){$headers['X-CSRF-Token']=$cookie.Value}};$headers['Idempotency-Key']=[guid]::NewGuid().ToString();return (Invoke-RestMethod "$base/api$path" -Method Post -WebSession $session -Headers $headers -ContentType application/json -Body ($body|ConvertTo-Json -Depth 30 -Compress)).data}
$sales=Login 's1@d5.example.test';$buyer=Login 'p1@d5.example.test';$shipping=Login 'l1@d5.example.test'
$existing=Request $sales '/inquiry-workspace' @{action='list';view='QUOTATIONS';page=1;size=100;keyword=$FixtureTitle}
$case=$null
if($existing.items.Count){$case=$existing.items[0]}
$products=@(
 @{id='gap-a';product='产品 A 钢板';specification='Q235 / 8mm';quantity='1';unit='MT';delivery='2026-10-30';weight='1';volume='';packaging='托盘';packageQuantity='1';remark='C/D 验收';customFields=@{}},
 @{id='gap-b';product='产品 B 钢卷';specification='SPCC / 1mm';quantity='1';unit='MT';delivery='2026-10-30';weight='1';volume='';packaging='防水包装';packageQuantity='1';remark='C/D 验收';customFields=@{}},
 @{id='gap-c';product='产品 C 钢管';specification='Q235 / 60mm';quantity='1';unit='MT';delivery='2026-10-30';weight='1';volume='';packaging='捆扎';packageQuantity='1';remark='C/D 验收';customFields=@{}}
)
if(!$case){
$body=@{title=$FixtureTitle;customerId='19';customer='D6 验收客户';contactId='';contact='验收联系人';delivery='2026-10-30';loadingPort='Shanghai';destinationPort='Hamburg';incoterm='CFR';remark='固定人工验收案例';products=$products;attachments=@()}
$created=Request $sales '/inquiry-workspace' @{action='save';view='SALES';id='';revision=0;body=$body} $true
$case=$created.item
$submitted=Request $sales '/inquiry-workspace' @{action='submit';view='SALES';id=$case.id;revision=$case.revision} $true
$case=$submitted.item
}
$detail=Request $sales '/inquiry-workspace' @{action='get';view='QUOTATIONS';id=$case.id}
$case=$detail.item
$products=@($case.body.products)
function Prices([string]$factory,[string]$fob,[string]$slitting){return @($products|ForEach-Object {@{productId=$_.id;price=$factory;delivery='2026-10-20';remark='工厂单价'}})}
if($case.procurementCount -eq 0){
$cny=@{company='上海钢材供应商';currency='CNY';delivery='2026-10-20';validUntil='2026-10-15';paymentTerms='30% 预付';incoterm='FOB';remark='工厂统一报价';prices=(Prices '700' '700' '14');carrier='';route='';vessel='';voyage='';departure='';arrival='';transitDays='';loadingPort='';destinationPort='';cargoIds=@();charges=@();totals=@{};attachments=@()}
$r=Request $buyer '/inquiry-workspace' @{action='quote';view='PROCUREMENT';id=$case.id;revision=$case.revision;quoteId='';quoteVersion=0;submit=$true;quote=$cny} $true;$case=$r.item
 }elseif($case.procurementCount -ne 1){throw 'Expected one factory quote'}
$productAId=$products[0].id

if($case.logisticsCount -eq 0){
$charges=@(
 @{name='内陆运费';amount='70';currency='CNY';unit='MT';quantity='1';subtotal='70';allocationType='PER_TON'},
 @{name='港杂费';amount='35';currency='CNY';unit='MT';quantity='1';subtotal='35';allocationType='PER_TON'},
 @{name='短导费';amount='21';currency='CNY';unit='MT';quantity='1';subtotal='21';allocationType='PER_TON'},
 @{name='分条费';amount='14';currency='CNY';unit='MT';quantity='1';subtotal='14';allocationType='PER_TON'},
 @{name='海运费';amount='70.5';currency='CNY';unit='MT';quantity='1';subtotal='70.5';allocationType='PER_TON'}
)
$q=@{company='上海远航货代';currency='CNY';incoterm='';prices=@();charges=$charges;transitDays='30';loadingPort='Shanghai';destinationPort='Hamburg';route='上海至汉堡';carrier='验收船公司';departure='2026-10-01';arrival='2026-10-31';cargoIds=@();attachments=@();totals=@{}}
$r=Request $shipping '/inquiry-workspace' @{action='quote';view='LOGISTICS';id=$case.id;revision=$case.revision;quoteId='';quoteVersion=0;submit=$true;quote=$q} $true
$case=$r.item
}
$offer=Request $sales '/customer-offer' @{action='get';caseId=$case.id;revision=0;body=@{}}
$evidence=[ordered]@{caseId=$case.id;caseNumber=$case.number;title=$case.body.title;products=$products.Count;procurementQuotes=$case.procurementCount;logisticsQuotes=$case.logisticsCount;offerStatus=$offer.status;salesUrl="http://127.0.0.1:25475/sales/quotations?id=$($case.id)";preparedAt=(Get-Date).ToString('o')}
$evidence|ConvertTo-Json -Depth 8|Set-Content (Join-Path $PSScriptRoot 'quote-e2e-fixture.json') -Encoding utf8
$evidence|ConvertTo-Json -Compress

$offer=Request $sales '/customer-offer' @{action='get';caseId=$case.id;body=@{}}
if($offer.status -eq 'CONFIRMED'){throw 'This regression fixture is already confirmed; use a fresh fixture title'}
$b=$offer.body;$b.currency='USD';$b.quoteFx='7.05';$b.quoteFxConfirmed=$true;$b.incoterm='CFR'
$logistics=@($offer.source.quotes|Where-Object kind -eq 'LOGISTICS')[0];$factory=@($offer.source.quotes|Where-Object kind -eq 'PROCUREMENT')[0]
$b|Add-Member -NotePropertyName logisticsQuoteId -NotePropertyValue $logistics.id -Force
$quantities=@{};foreach($line in $b.lines){$line.calculation.formula=1;$line.calculation.mtPerUnit='1';$line.factoryQuoteId=$factory.id;$quantities[$line.id]=$line.quantity}
$b.transports=@(@{quoteId=$logistics.id;title='自动回归物流';currency='CNY';price='210.50';accepted=$true;quantities=$quantities})
$priced=Request $sales '/customer-offer' @{action='calculate_all';caseId=$case.id;revision=$offer.revision;body=$b}
$priced.body.lines[0].unitPrice='118'
$saved=Request $sales '/customer-offer' @{action='save';caseId=$case.id;revision=$offer.revision;body=$priced.body}
$loaded=Request $sales '/customer-offer' @{action='get';caseId=$case.id;body=@{}}
@{stale=$loaded.pricingStale;price=$loaded.body.lines[0].unitPrice;total=$loaded.body.total}|ConvertTo-Json
if($loaded.pricingStale -or $loaded.body.lines[0].unitPrice -ne '118.00' -or $loaded.body.total -ne '348.00'){throw 'Saved pricing snapshot or negotiated price failed'}
$pdf=Request $sales '/customer-offer' @{action='pdf';caseId=$case.id;body=@{}}
if(!$pdf.fileData){throw 'PDF failed'}
$confirmed=Request $sales '/customer-offer' @{action='confirm';caseId=$case.id;revision=$saved.revision;body=$saved.body}
if($confirmed.status -ne 'CONFIRMED' -or [long]$confirmed.contractId -le 0 -or $confirmed.canEdit){throw 'Confirmation lock failed'}
$locked=$false
try { Request $sales '/customer-offer' @{action='save';caseId=$case.id;revision=$confirmed.revision;body=$confirmed.body}|Out-Null } catch {if($_.ErrorDetails.Message -match 'OFFER_CONFIRMED'){$locked=$true}else{throw}}
if(!$locked){throw 'Confirmed offer was editable'}
@{caseId=$case.id;contractId=$confirmed.contractId;total=$confirmed.body.total;negotiatedPricePreserved=$true;pdfPassed=$true;confirmationLocked=$locked}|ConvertTo-Json|Tee-Object -FilePath (Join-Path $PSScriptRoot 'quote-e2e-result.json')

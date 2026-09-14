$ErrorActionPreference='Stop'
$base='http://127.0.0.1:28382';$password='D5-Acceptance-2026!'
function Login([string]$email){Invoke-RestMethod "$base/api/auth/login" -Method Post -ContentType application/json -Body (@{email=$email;password=$password}|ConvertTo-Json) -SessionVariable session|Out-Null;return $session}
function Request($session,[string]$path,$body,[bool]$idempotent=$false){$headers=@{};foreach($cookie in $session.Cookies.GetCookies([uri]$base)){if($cookie.Name-eq'erp_csrf'){$headers['X-CSRF-Token']=$cookie.Value}};if($idempotent){$headers['Idempotency-Key']=[guid]::NewGuid().ToString()};return (Invoke-RestMethod "$base/api$path" -Method Post -WebSession $session -Headers $headers -ContentType application/json -Body ($body|ConvertTo-Json -Depth 30 -Compress)).data}
$sales=Login 's1@d5.example.test';$buyer=Login 'p1@d5.example.test';$shipping=Login 'l1@d5.example.test'
$existing=Request $sales '/inquiry-workspace' @{action='list';view='QUOTATIONS';page=1;size=100;keyword='GAP-CD-ACCEPT-001'}
$case=$null
if($existing.items.Count){$case=$existing.items[0]}
$products=@(
 @{id='gap-a';product='产品 A 钢板';specification='Q235 / 8mm';quantity='1';unit='MT';delivery='2026-10-30';weight='1';volume='';packaging='托盘';packageQuantity='1';remark='C/D 验收';customFields=@{}},
 @{id='gap-b';product='产品 B 钢卷';specification='SPCC / 1mm';quantity='1';unit='MT';delivery='2026-10-30';weight='1';volume='';packaging='防水包装';packageQuantity='1';remark='C/D 验收';customFields=@{}},
 @{id='gap-c';product='产品 C 钢管';specification='Q235 / 60mm';quantity='1';unit='MT';delivery='2026-10-30';weight='1';volume='';packaging='捆扎';packageQuantity='1';remark='C/D 验收';customFields=@{}}
)
if(!$case){
$body=@{title='GAP-CD-ACCEPT-001 五公式与物流分摊验收';customerId='19';customer='D6 验收客户';contactId='';contact='验收联系人';delivery='2026-10-30';loadingPort='Shanghai';destinationPort='Hamburg';incoterm='CFR';remark='固定人工验收案例';products=$products;attachments=@()}
$created=Request $sales '/inquiry-workspace' @{action='save';view='SALES';id='';revision=0;body=$body} $true
$case=$created.item
$submitted=Request $sales '/inquiry-workspace' @{action='submit';view='SALES';id=$case.id;revision=$case.revision} $true
$case=$submitted.item
}
$detail=Request $sales '/inquiry-workspace' @{action='get';view='QUOTATIONS';id=$case.id}
$case=$detail.item
$products=@($case.body.products)
function Prices([string]$factory,[string]$fob,[string]$slitting){return @($products|ForEach-Object {@{productId=$_.id;price=$factory;factoryPrice=$factory;fobPrice=$fob;slitting=$slitting;delivery='2026-10-20';remark='验收固定价格'}})}
if($case.procurementCount -eq 0){
$cny=@{company='人民币核价工厂';currency='CNY';delivery='2026-10-20';validUntil='2026-10-15';paymentTerms='30% 预付';incoterm='FOB';remark='用于公式 1、3、4、5';prices=(Prices '700' '700' '14');carrier='';route='';vessel='';voyage='';departure='';arrival='';transitDays='';loadingPort='';destinationPort='';cargoIds=@();charges=@();totals=@{};attachments=@()}
$r=Request $buyer '/inquiry-workspace' @{action='quote';view='PROCUREMENT';id=$case.id;revision=$case.revision;quoteId='';quoteVersion=0;submit=$true;quote=$cny} $true;$case=$r.item
$usd=@{company='美元FOB核价工厂';currency='USD';delivery='2026-10-20';validUntil='2026-10-15';paymentTerms='30% 预付';incoterm='FOB';remark='用于公式 2';prices=(Prices '100' '100' '2');carrier='';route='';vessel='';voyage='';departure='';arrival='';transitDays='';loadingPort='';destinationPort='';cargoIds=@();charges=@();totals=@{};attachments=@()}
$r=Request $buyer '/inquiry-workspace' @{action='quote';view='PROCUREMENT';id=$case.id;revision=$case.revision;quoteId='';quoteVersion=0;submit=$true;quote=$usd} $true;$case=$r.item
}elseif($case.procurementCount -ne 2){throw "Expected zero or two procurement quotes, found $($case.procurementCount)"}
$productAId=$products[0].id
$charges=@(
 @{name='产品 A 专属费用';amount='20';currency='CNY';unit='整项';quantity='1';subtotal='20';remark='只计产品 A';allocationType='DIRECT';productId=$productAId},
 @{name='按吨操作费';amount='3';currency='EUR';unit='MT';quantity='1';subtotal='3';remark='每吨计费';allocationType='PER_TON';productId=''},
 @{name='整票固定费';amount='1';currency='USD';unit='整票';quantity='1';subtotal='1';remark='按重量比例分摊';allocationType='FIXED';productId=''},
 @{name='短导费';amount='21';currency='CNY';unit='MT';quantity='1';subtotal='21';remark='公式核价来源';allocationType='PER_TON';productId=''},
 @{name='内陆运费';amount='70';currency='CNY';unit='MT';quantity='1';subtotal='70';remark='公式核价来源';allocationType='PER_TON';productId=''},
 @{name='港杂费';amount='35';currency='CNY';unit='MT';quantity='1';subtotal='35';remark='公式核价来源';allocationType='PER_TON';productId=''},
 @{name='海运费';amount='10';currency='USD';unit='MT';quantity='1';subtotal='10';remark='公式核价来源';allocationType='PER_TON';productId=''}
)
$formulaCharges=@($charges|Where-Object {$_.name -in @('短导费','内陆运费','港杂费','海运费')})
$allocationCharges=@($charges|Where-Object {$_.name -in @('产品 A 专属费用','按吨操作费','整票固定费')})
if($case.logisticsCount -eq 0){
$formulaLogistics=@{company='公式核价物流报价';currency='USD';delivery='';validUntil='2026-10-15';paymentTerms='装船后付款';incoterm='CFR';remark='短导21、内陆70、港杂35、海运10、运输30天';prices=@();carrier='验收船公司';route='C 公式核价专用';vessel='ACCEPTANCE';voyage='V001';departure='2026-10-01';arrival='2026-10-30';transitDays='30';loadingPort='Shanghai';destinationPort='Hamburg';cargoIds=@();charges=$formulaCharges;totals=@{};attachments=@()}
$r=Request $shipping '/inquiry-workspace' @{action='quote';view='LOGISTICS';id=$case.id;revision=$case.revision;quoteId='';quoteVersion=0;submit=$true;quote=$formulaLogistics} $true;$case=$r.item
$allocationLogistics=@{company='费用分摊物流报价';currency='USD';delivery='';validUntil='2026-10-15';paymentTerms='装船后付款';incoterm='CFR';remark='产品A专属CNY20、每吨EUR3、整票USD1';prices=@();carrier='验收船公司';route='D 费用分摊专用';vessel='ACCEPTANCE';voyage='V002';departure='2026-10-02';arrival='2026-10-31';transitDays='29';loadingPort='Shanghai';destinationPort='Hamburg';cargoIds=@();charges=$allocationCharges;totals=@{};attachments=@()}
$r=Request $shipping '/inquiry-workspace' @{action='quote';view='LOGISTICS';id=$case.id;revision=$case.revision;quoteId='';quoteVersion=0;submit=$true;quote=$allocationLogistics} $true;$case=$r.item
}elseif($case.logisticsCount -ne 2){throw "Expected zero or two logistics quotes, found $($case.logisticsCount)"}
$offer=Request $sales '/customer-offer' @{action='get';caseId=$case.id;revision=0;body=@{}}
$evidence=[ordered]@{caseId=$case.id;caseNumber=$case.number;title=$case.body.title;products=$products.Count;procurementQuotes=$case.procurementCount;logisticsQuotes=$case.logisticsCount;offerStatus=$offer.status;salesUrl="http://127.0.0.1:25475/sales/quotations?id=$($case.id)";preparedAt=(Get-Date).ToString('o')}
$evidence|ConvertTo-Json -Depth 8|Set-Content (Join-Path $PSScriptRoot 'offer-allocation-acceptance-evidence.json') -Encoding utf8
$evidence|ConvertTo-Json -Compress

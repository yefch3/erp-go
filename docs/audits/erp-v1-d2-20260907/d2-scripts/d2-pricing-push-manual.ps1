. (Join-Path $PSScriptRoot 'api-helpers.ps1')

function Assert($value,[string]$message){if(!$value){throw $message};"PASS $message"}

$s=Login 's1@d2.example.test' 'D2-Acceptance-2026!'
$p=Login 'p1@d2.example.test' 'D2-Acceptance-2026!'
$l=Login 'l1@d2.example.test' 'D2-Acceptance-2026!'
$admin=Login 'admin@d2.example.test' 'admin123'
Api $admin POST '/fx/effective' @{base_currency='USD';quote_currency='CNY';rate='7.05';remark='D2 manual formula acceptance'}|Out-Null

$inquiry=(Api $s POST '/inquiry-workspace' @{action='save';view='SALES';body=@{
 customer='D2 上游核算推送人工验收'
 contact='验收联系人'
 loadingPort='上海港'
 destinationPort='天津港'
 incoterm='FOB'
 delivery='2026-10-15'
 products=@(
  @{product='冷轧卷';specification='测试标准A / 1250mm';quantity='10';unit='MT'},
  @{product='热轧钢板';specification='测试标准B / 1500×6000mm';quantity='5';unit='MT'},
  @{product='焊接钢管';specification='测试标准C / 60mm';quantity='3';unit='MT'}
 )
}}).item
$inquiry=(Api $s POST '/inquiry-workspace' @{action='submit';view='SALES';id=$inquiry.id;revision=$inquiry.revision}).item

$prices=@(
 @{productId=$inquiry.body.products[0].id;price='700';delivery='2026-10-10';remark='FOB RMB'},
 @{productId=$inquiry.body.products[1].id;price='805';delivery='2026-10-12';remark='FOB RMB'},
 @{productId=$inquiry.body.products[2].id;price='920';delivery='2026-10-14';remark='FOB RMB'}
)
Api $p POST '/inquiry-workspace' @{action='quote';view='PROCUREMENT';id=$inquiry.id;revision=$inquiry.revision;submit=$true;quote=@{company='D2 验收工厂';currency='CNY';incoterm='FOB';prices=$prices}}|Out-Null

foreach($plan in @(
 @{company='D2 物流方案 A';carrier='测试船公司 A';route='直航';amount='10';days='18'},
 @{company='D2 物流方案 B';carrier='测试船公司 B';route='中转';amount='12';days='24'}
)){
 Api $l POST '/inquiry-workspace' @{action='quote';view='LOGISTICS';id=$inquiry.id;revision=$inquiry.revision;submit=$true;quote=@{
  company=$plan.company;carrier=$plan.carrier;route=$plan.route;currency='USD';incoterm='CFR'
  transitDays=$plan.days;charges=@(@{name='海运费';currency='USD';amount=$plan.amount;quantity='18';unit='MT'})
 }}|Out-Null
}

$offer=Api $s POST '/customer-offer' @{action='get';caseId=$inquiry.id}
Assert (@($offer.source.quotes|Where-Object kind -eq 'PROCUREMENT').Count -eq 1) 'Factory quote reached sales'
Assert (@($offer.source.quotes|Where-Object kind -eq 'LOGISTICS').Count -eq 2) 'Two logistics quotes reached sales'
$factory=@($offer.source.quotes|Where-Object kind -eq 'PROCUREMENT')[0]
foreach($line in $offer.body.lines){
 $line.factoryQuoteId=$factory.id
 $line.calculation.formula=1
 $line.calculation.ocean='10'
 $line.calculation.days='30'
 $line.calculation.mtPerUnit='1'
}
$calculated=Api $s POST '/customer-offer' @{action='calculate_all';caseId=$inquiry.id;revision=$offer.revision;body=$offer.body}
$calculatedPrices=@($calculated.body.lines|ForEach-Object unitPrice)
Assert (($calculatedPrices -join ',') -eq '115.00,130.00,146.43') 'One formula calculates all product prices in one request'
foreach($line in $offer.body.lines){
 $line.calculation.formula=2
 $line.calculation.ocean='10'
 $line.calculation.days='10'
 $line.calculation.mtPerUnit='1'
}
$usdFormula=Api $s POST '/customer-offer' @{action='calculate_all';caseId=$inquiry.id;revision=$offer.revision;body=$offer.body}
$usdFormulaPrices=@($usdFormula.body.lines|ForEach-Object unitPrice)
Assert (($usdFormulaPrices -join ',') -eq '110.96,125.85,142.16') 'USD formula accepts converted CNY factory prices'

$evidence=[ordered]@{
 inquiryId=$inquiry.id
 inquiryNumber=$inquiry.number
 url="http://127.0.0.1:25174/sales/quotations?id=$($inquiry.id)"
 factoryPrices=@('CNY 700','CNY 805','CNY 920')
 logisticsPlans=@('方案 A：USD 10/MT，18 天','方案 B：USD 12/MT，24 天')
 formulaOneExample='产品 1 选择公式 1：700 ÷ (7.05 - 0.05) + 10 + 5 ÷ 30 × 30 = USD 115.00/MT'
 calculatedPrices=$calculatedPrices
 usdFormulaPrices=$usdFormulaPrices
 checkedAt=[DateTimeOffset]::Now.ToString('o')
}
$evidence|ConvertTo-Json|Set-Content (Join-Path $PSScriptRoot '../evidence/d2-pricing-push-manual.json')
$evidence|ConvertTo-Json

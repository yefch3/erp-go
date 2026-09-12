. (Join-Path $PSScriptRoot 'api-helpers.ps1')
function Assert($value,[string]$message){if(!$value){throw $message};"PASS $message"}
function Denied($session,$method,$path,$body){try{Api $session $method $path $body|Out-Null}catch{if([int]$_.Exception.Response.StatusCode -in @(400,403,404,409)){return};throw};throw "Unexpected authorization: $path"}
$s=Login 's1@d2.example.test' 'D2-Acceptance-2026!'
$p=Login 'p1@d2.example.test' 'D2-Acceptance-2026!'
$l=Login 'l1@d2.example.test' 'D2-Acceptance-2026!'
$admin=Login 'admin@d2.example.test' 'admin123'
Api $admin POST '/fx/effective' @{base_currency='USD';quote_currency='CNY';rate='7.05';remark='D2 isolated acceptance'}|Out-Null
Denied $s POST '/fx/effective' @{base_currency='USD';quote_currency='CNY';rate='7.10'}
$r=(Api $s POST '/inquiry-workspace' @{action='save';view='SALES';body=@{customer='D2 multiple plans';contact='Test contact';delivery='2026-10-01';products=@(@{product='Steel';specification='Q235';quantity='100';unit='MT'})}}).item
$r=(Api $s POST '/inquiry-workspace' @{action='submit';view='SALES';id=$r.id;revision=$r.revision}).item
foreach($factory in @('Factory A','Factory B')){Api $p POST '/inquiry-workspace' @{action='quote';view='PROCUREMENT';id=$r.id;revision=$r.revision;submit=$true;quote=@{company=$factory;currency='CNY';incoterm='FOB';prices=@(@{productId=$r.body.products[0].id;price='700'})}}|Out-Null}
foreach($carrier in @('Plan A','Plan B')){Api $l POST '/inquiry-workspace' @{action='quote';view='LOGISTICS';id=$r.id;revision=$r.revision;submit=$true;quote=@{company=$carrier;carrier=$carrier;currency='USD';incoterm='CFR';charges=@(@{name='Ocean';currency='USD';amount='10';quantity='100';unit='MT'})}}|Out-Null}
$o=Api $s POST '/customer-offer' @{action='get';caseId=$r.id}
Assert ($o.source.quotes.Count -eq 4) 'Two factories and two forwarders received'
$line=$o.body.lines[0];$line.quantity='60';$line.factoryQuoteId=($o.source.quotes|Where-Object kind -eq 'PROCUREMENT'|Select-Object -First 1).id
$line.calculation=@{formula=1;factory='1';ocean='10';days='30';mtPerUnit='1'}
$o=Api $s POST '/customer-offer' @{action='calculate';caseId=$r.id;revision=$o.revision;lineId=$line.id;body=$o.body}
Assert ($o.body.lines[0].unitPrice -eq '115.00') 'Sales-selected formula uses factory quote, logistics fee and effective rate'
$o.body.lines[0].unitPrice='120'
$o.body.transports=@($o.source.quotes|Where-Object kind -eq 'LOGISTICS'|ForEach-Object {@{quoteId=$_.id;title=$_.body.company;currency='USD';price='1000';accepted=$false;quantities=@{}}})
Assert ($o.body.transports.Count -eq 2) 'Two customer-facing candidate plans'
$o.body.transports[0].accepted=$true;$o.body.transports[0].quantities[$line.id]='60'
$o=Api $s POST '/customer-offer' @{action='save';caseId=$r.id;revision=$o.revision;body=$o.body}
Assert ($o.body.total -eq '7200.00') 'Negotiated final price replaces calculation without changing inquiry'
$o=Api $s POST '/customer-offer' @{action='confirm';caseId=$r.id;revision=$o.revision}
$c=Api $s GET "/contracts/$($o.contractId)" $null
$snapshot=$c.acceptedOfferJson|ConvertFrom-Json
Assert ($snapshot.contact -eq 'Test contact' -and @($snapshot.transports|Where-Object accepted).Count -eq 1) 'Contract preserves contact and one accepted plan'
Assert ($snapshot.transports[0].quantities.($line.id) -eq '60') 'Accepted plan carries negotiated quantity'
Assert ($c.acceptedOfferJson -notmatch 'calculation|factoryQuoteId') 'Contract plan payload excludes internal calculation'
Api $admin POST '/fx/effective' @{base_currency='USD';quote_currency='CNY';rate='7.20';remark='D2 changed reference after confirmation'}|Out-Null
$frozen=Api $s POST '/customer-offer' @{action='get';caseId=$r.id}
Assert ($frozen.body.total -eq '7200.00' -and [decimal]$frozen.body.rates[0].value -eq [decimal]'7.05') 'Confirmed offer unaffected by subsequent rate change'
@{inquiryId=$r.id;contractId=$o.contractId;factoryQuotes=2;transportOptions=2;acceptedOptions=1;total='7200.00';checkedAt=(Get-Date).ToUniversalTime().ToString('o')}|ConvertTo-Json|Set-Content (Join-Path $PSScriptRoot '../evidence/d2-multiple-plans.json')

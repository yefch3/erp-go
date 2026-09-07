. "$PSScriptRoot/acceptance.ps1" -SeedOnly
function Denied($s,$body,[int[]]$codes=@(403,404,409)){
 try{Api $s POST '/inquiry-workspace' $body|Out-Null;throw 'Unexpected permission success'}catch [Microsoft.PowerShell.Commands.HttpResponseException]{if([int]$_.Exception.Response.StatusCode -notin $codes){throw}}
}
$s=$people.S1.session;$p=$people.P1.session;$p2=$people.P2.session;$l=$people.L1.session;$l2=$people.L2.session
$products=1..126|ForEach-Object {@{product="Steel $_";specification="Q235 $($_)mm";quantity="$_";unit='MT'}}
$r=(Api $s POST '/inquiry-workspace' @{action='save';view='SALES';body=@{customer='D1 126 products';products=@($products)}}).item
$upload=(Api $s POST '/inquiry-workspace' @{action='upload';view='SALES';id=$r.id;revision=$r.revision;fileName='D1-source.txt';fileData=[Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes('D1 source attachment'))}).attachment
$r.body.attachments=@($upload)
$r=(Api $s POST '/inquiry-workspace' @{action='save';view='SALES';id=$r.id;revision=$r.revision;body=$r.body}).item
Denied $people.S2.session @{action='get';view='SALES';id=$r.id}
Denied $p @{action='get';view='PROCUREMENT';id=$r.id}
$mgr=(Api $people.M1.session POST '/inquiry-workspace' @{action='get';view='SALES';id=$r.id}).item
if($mgr.canEdit){throw 'Manager acquired ownership'}
$boss=(Api $people.B1.session POST '/inquiry-workspace' @{action='get';view='SALES';id=$r.id}).item
if($boss.canEdit){throw 'Boss acquired ownership'}
Denied $people.B1.session @{action='save';view='SALES';id=$r.id;revision=$r.revision;body=$r.body}
Denied $people.M1.session @{action='save';view='SALES';id=$r.id;revision=$r.revision;body=$r.body}
$r=(Api $s POST '/inquiry-workspace' @{action='submit';view='SALES';id=$r.id;revision=$r.revision}).item
$download=Api $p POST '/inquiry-workspace' @{action='download';view='PROCUREMENT';id=$r.id;fileKey=$upload.key}
$file=Invoke-WebRequest ($base+$download.url) -WebSession $p
if($file.StatusCode -ne 200){throw 'Source attachment download failed'}
try{Invoke-WebRequest ($base+$download.url) -WebSession $people.S2.session|Out-Null;throw 'Copied download link granted access'}catch [Microsoft.PowerShell.Commands.HttpResponseException]{if([int]$_.Exception.Response.StatusCode -notin @(403,404)){throw}}
Denied $people.S2.session @{action='download';view='SALES';id=$r.id;fileKey=$upload.key}
'Attachments: submitted source downloadable by procurement; unrelated sales denied'
$prices=$r.body.products|ForEach-Object {@{productId=$_.id;price='12.34';delivery='2026-10-01';remark='126 roundtrip'}}
$q=@{company='D1 Factory';currency='USD';prices=@($prices);attachments=@()}
$b=(Api $p POST '/inquiry-workspace' @{action='quote';view='PROCUREMENT';id=$r.id;revision=$r.revision;quote=$q}).item
$quote=$b.quotes[0]
Denied $p2 @{action='quote';view='PROCUREMENT';id=$r.id;revision=$r.revision;quoteId=$quote.id;quoteVersion=$quote.version;quote=$q}
$b=(Api $p POST '/inquiry-workspace' @{action='quote';view='PROCUREMENT';id=$r.id;revision=$r.revision;quoteId=$quote.id;quoteVersion=$quote.version;quote=$q;submit=$true}).item
$quote=$b.quotes[0];$q.prices[125].price='98.76'
$b=(Api $p2 POST '/inquiry-workspace' @{action='quote';view='PROCUREMENT';id=$r.id;revision=$r.revision;quoteId=$quote.id;quoteVersion=$quote.version;quote=$q}).item
$received=(Api $s POST '/inquiry-workspace' @{action='get';view='QUOTATIONS';id=$r.id}).item
if($received.quotes[0].body.prices.Count -ne 126 -or $received.quotes[0].body.prices[125].price -ne '98.76' -or $received.quotes[0].updatedBy -ne 'P2'){throw '126 products/co-worker update mismatch'}
'A06 126 prices saved, submitted and updated by P2; S1 receives exact last-row price'
$logq=@{company='D1 Forwarder';carrier='D1 Carrier';route='Plan A';cargoIds=@($r.body.products[0].id);charges=@(@{name='Ocean';amount='10.05';quantity='3';currency='USD';unit='container'},@{name='Port';amount='21.11';quantity='2';currency='CNY';unit='shipment'})}
$lr=(Api $l POST '/inquiry-workspace' @{action='quote';view='LOGISTICS';id=$r.id;revision=$r.revision;quote=$logq}).item
$lq=$lr.quotes[0]
$received=(Api $s POST '/inquiry-workspace' @{action='get';view='QUOTATIONS';id=$r.id}).item
if($received.logisticsCount -ne 0){throw 'Logistics save leaked'}
$lr=(Api $l POST '/inquiry-workspace' @{action='quote';view='LOGISTICS';id=$r.id;revision=$r.revision;quoteId=$lq.id;quoteVersion=$lq.version;quote=$logq;submit=$true}).item
$lq=$lr.quotes[0];$logq.charges[0].amount='20.05'
$lr=(Api $l2 POST '/inquiry-workspace' @{action='quote';view='LOGISTICS';id=$r.id;revision=$r.revision;quoteId=$lq.id;quoteVersion=$lq.version;quote=$logq}).item
$received=(Api $s POST '/inquiry-workspace' @{action='get';view='QUOTATIONS';id=$r.id}).item
$log=$received.quotes|Where-Object {$_.kind -eq 'LOGISTICS'}
if($log.body.totals.USD -ne '60.15' -or $log.body.totals.CNY -ne '42.22' -or $log.updatedBy -ne 'L2'){throw 'Logistics totals/update mismatch'}
$logq.route='Plan B';Api $l POST '/inquiry-workspace' @{action='quote';view='LOGISTICS';id=$r.id;revision=$r.revision;quote=$logq;submit=$true}|Out-Null
'A07-A09 same forwarder two plans, optional dates, partial cargo and USD/CNY totals passed'
Denied $s @{action='quote';view='PROCUREMENT';id=$r.id;revision=$r.revision;quote=$q}
Denied $people.U1.session @{action='get';view='SALES';id=$r.id}
try{Api $p POST "/sourcing-cases/$($r.id)/participants" @{}|Out-Null;throw 'Retired participation accepted'}catch [Microsoft.PowerShell.Commands.HttpResponseException]{if([int]$_.Exception.Response.StatusCode -ne 409){throw}}
'A14/A16 sales write-back, unprivileged access and retired participation rejected'
$r|ConvertTo-Json -Depth 30|Set-Content "$PSScriptRoot/ui-inquiry.json" -Encoding utf8
"UI acceptance fixture inquiry=$($r.id), 126 products, P2/L2 edits and two logistics plans"

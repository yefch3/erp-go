. "$PSScriptRoot/acceptance.ps1" -SeedOnly
$x=Login 'x@d1-other.example.test' 'D1-Acceptance-2026!'
try{Api $x POST '/inquiry-workspace' @{action='get';view='SALES';id='6'}|Out-Null;throw 'Cross-tenant inquiry exposed'}catch [Microsoft.PowerShell.Commands.HttpResponseException]{if([int]$_.Exception.Response.StatusCode -ne 404){throw}}
$items=Api $x POST '/inquiry-workspace' @{action='list';view='SALES';page=1;size=100}
if($items.total -ne 0){throw 'Cross-tenant list exposed'}
'A14 real tenant 22001 SALES/SELF login: tenant 1 inquiry and list inaccessible'
$s=$people.S1.session
$r=(Api $s POST '/inquiry-workspace' @{action='save';view='SALES';body=@{customer='D1 failure retry';products=@(@{product='Steel';quantity='1';unit='MT'})}}).item
$r=(Api $s POST '/inquiry-workspace' @{action='submit';view='SALES';id=$r.id;revision=$r.revision}).item
Api $people.P1.session POST '/inquiry-workspace' @{action='quote';view='PROCUREMENT';id=$r.id;revision=$r.revision;submit=$true;quote=@{company='Retry factory';currency='USD';incoterm='FOB';prices=@(@{productId=$r.body.products[0].id;price='2.34'})}}|Out-Null
docker stop erp-d1-20260906-export-1|Out-Null
try{
 try{Api $s POST '/inquiry-workspace' @{action='withdraw';view='SALES';id=$r.id;revision=$r.revision}|Out-Null;throw 'Export failure reported success'}catch [Microsoft.PowerShell.Commands.HttpResponseException]{if([int]$_.Exception.Response.StatusCode -notin @(500,503,504)){throw}}
 $unchanged=(Api $s POST '/inquiry-workspace' @{action='get';view='SALES';id=$r.id}).item
 if($unchanged.state -ne 'INQUIRING' -or $unchanged.quotes.Count -ne 1){throw 'Failed withdrawal changed source'}
}finally{docker start erp-d1-20260906-export-1|Out-Null}
# Wait only for the dedicated export process to accept RPC connections.
$success=$false
for($i=0;$i -lt 90;$i++){try{$r=(Api $s POST '/inquiry-workspace' @{action='withdraw';view='SALES';id=$r.id;revision=$r.revision}).item;$success=$true;break}catch [Microsoft.PowerShell.Commands.HttpResponseException]{if([int]$_.Exception.Response.StatusCode -notin @(500,503,504)){throw};Start-Sleep -Milliseconds 500}}
if(!$success -or $r.state -ne 'WITHDRAWN' -or $r.quotes.Count -ne 0){throw 'Retry failed'}
"A13 export unavailable preserved source; service restored, withdrawal retry cleared quote inquiry=$($r.id)"

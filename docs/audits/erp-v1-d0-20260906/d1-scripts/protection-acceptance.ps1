. "$PSScriptRoot/acceptance.ps1" -SeedOnly
function D1Sql([string]$db,[string]$sql){$result=$sql|docker exec -i erp-d1-20260906-postgres-1 psql -v ON_ERROR_STOP=1 -U "erp_$db" -d "erp_$db" -t -A;if($LASTEXITCODE){throw "D1 $db SQL failed"};return ($result -join "`n").Trim()}
function NewInquiry($label){$r=(Api $people.S1.session POST '/inquiry-workspace' @{action='save';view='SALES';body=@{customer=$label;products=@(@{product='D1 steel';quantity='3';unit='MT'})}}).item;return (Api $people.S1.session POST '/inquiry-workspace' @{action='submit';view='SALES';id=$r.id;revision=$r.revision}).item}
function RejectWithdrawal($r){try{Api $people.S1.session POST '/inquiry-workspace' @{action='withdraw';view='SALES';id=$r.id;revision=$r.revision}|Out-Null;throw 'Protected inquiry withdrawn'}catch [Microsoft.PowerShell.Commands.HttpResponseException]{if([int]$_.Exception.Response.StatusCode -ne 409){throw}}}
foreach($kind in @('ACCEPTED','CONTRACT')){
 $r=NewInquiry "D1 protected $kind"
 $status=if($kind -eq 'ACCEPTED'){'ACCEPTED'}else{'DRAFT'}
 $qid=D1Sql 'export' "INSERT INTO quotations(tenant_id,quote_no,customer_id,currency,fx_rate,fx_rate_at,fx_source,source_sourcing_case_id,status) VALUES(1,'D1-protected-$($r.id)',1,'USD',1,now(),'D1',$($r.id),'$status') RETURNING id;"
 $qid=($qid -split "`n")[0]
 if($kind -eq 'CONTRACT'){D1Sql 'export' "INSERT INTO contracts(tenant_id,contract_no,quotation_id,customer_id) VALUES(1,'D1-protected-$($r.id)',$qid,1);"|Out-Null}
 RejectWithdrawal $r
 $count=D1Sql 'export' "SELECT count(*) FROM quotations WHERE id=$qid AND tenant_id=1;"
 if($count -ne '1'){throw 'Protected quote lost'}
 "A12 real gateway/export $kind protected inquiry=$($r.id) quotation=$qid"
}
$r=NewInquiry 'D1 post-export rollback'
$q=@{company='Retry factory';currency='USD';prices=@(@{productId=$r.body.products[0].id;price='10.05'})}
$attachment=(Api $people.P1.session POST '/inquiry-workspace' @{action='upload';view='PROCUREMENT';id=$r.id;revision=$r.revision;fileName='old-quote.txt';fileData=[Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes('old round quote'))}).attachment
$q.attachments=@($attachment)
Api $people.P1.session POST '/inquiry-workspace' @{action='quote';view='PROCUREMENT';id=$r.id;revision=$r.revision;quote=$q;submit=$true}|Out-Null
D1Sql 'export' "INSERT INTO quotations(tenant_id,quote_no,customer_id,currency,fx_rate,fx_rate_at,fx_source,source_sourcing_case_id,status) VALUES(1,'D1-retry-$($r.id)',1,'USD',1,now(),'D1',$($r.id),'DRAFT');"|Out-Null
$sql='CREATE FUNCTION d1_test_fail_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF OLD.tenant_id=1 AND OLD.case_id=CASE_ID THEN RAISE EXCEPTION ''D1 injected local rollback''; END IF; RETURN OLD; END $$; CREATE TRIGGER d1_test_fail_delete BEFORE DELETE ON inquiry_quotes FOR EACH ROW EXECUTE FUNCTION d1_test_fail_delete();'
D1Sql 'procurement' $sql.Replace('CASE_ID',$r.id)|Out-Null
try{
 try{Api $people.S1.session POST '/inquiry-workspace' @{action='withdraw';view='SALES';id=$r.id;revision=$r.revision}|Out-Null;throw 'Local rollback reported success'}catch [Microsoft.PowerShell.Commands.HttpResponseException]{if([int]$_.Exception.Response.StatusCode -ne 500){throw}}
 $source=(Api $people.S1.session POST '/inquiry-workspace' @{action='get';view='SALES';id=$r.id}).item
 if($source.state -ne 'INQUIRING' -or $source.quotes.Count -ne 1){throw 'Local rollback lost source quote'}
 $fence=D1Sql 'export' "SELECT count(*) FROM inquiry_withdrawal_fences WHERE tenant_id=1 AND case_id=$($r.id);"
 if($fence -ne '0'){throw 'Automatic compensation did not restore export availability'}
}finally{D1Sql 'procurement' 'DROP TRIGGER d1_test_fail_delete ON inquiry_quotes; DROP FUNCTION d1_test_fail_delete();'|Out-Null}
$oldRevision=$r.revision
$r=(Api $people.S1.session POST '/inquiry-workspace' @{action='withdraw';view='SALES';id=$r.id;revision=$r.revision}).item
if($r.state -ne 'WITHDRAWN' -or $r.quotes.Count -ne 0){throw 'Post-export failure retry left quote'}
$r.body.products[0].quantity='7'
$r=(Api $people.S1.session POST '/inquiry-workspace' @{action='save';view='SALES';id=$r.id;revision=$r.revision;body=$r.body}).item
$r=(Api $people.S1.session POST '/inquiry-workspace' @{action='submit';view='SALES';id=$r.id;revision=$r.revision}).item
foreach($actor in @('P1','L1')){
 $view=if($actor -eq 'P1'){'PROCUREMENT'}else{'LOGISTICS'}
 try{Api $people[$actor].session POST '/inquiry-workspace' @{action='quote';view=$view;id=$r.id;revision=$oldRevision;quote=$q;submit=$true}|Out-Null;throw 'Old round accepted'}catch [Microsoft.PowerShell.Commands.HttpResponseException]{if([int]$_.Exception.Response.StatusCode -ne 409){throw}}
}
try{Api $people.P1.session POST '/inquiry-workspace' @{action='quote';view='PROCUREMENT';id=$r.id;revision=$r.revision;quote=$q;submit=$true}|Out-Null;throw 'Old attachment revived'}catch [Microsoft.PowerShell.Commands.HttpResponseException]{if([int]$_.Exception.Response.StatusCode -ne 403){throw}}
"A10-A13 export committed/local rollback auto-compensated; retry cleared both, resubmit qty=7, P/L old revision rejected, old attachment rejected inquiry=$($r.id)"

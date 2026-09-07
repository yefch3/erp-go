. "$PSScriptRoot/acceptance.ps1" -SeedOnly
$s=$people.S1.session
$fixture=Get-Content "$PSScriptRoot/ui-inquiry.json" -Raw|ConvertFrom-Json
$key=$fixture.body.attachments[0].key
if($key -notmatch '^inquiry/1/\d+/[a-f0-9]+$'){throw 'Unexpected fixture file key'}
$sql=@"
INSERT INTO email_inbound(tenant_id,account_id,owner_id,imap_uid,from_email,from_name,to_email,subject,body_text,has_attachments)
VALUES(1,1,2,987654,'client@d1.example.test','D1 Mail Contact','s1@d1.example.test','D1 source mail','Steel Q235 3 MT',true)
ON CONFLICT(tenant_id,account_id,folder,imap_uid) DO NOTHING;
INSERT INTO email_inbound_attachments(tenant_id,inbound_id,file_name,content_type,file_size,file_key)
SELECT 1,id,'D1-source.txt','text/plain',20,'$key' FROM email_inbound WHERE tenant_id=1 AND imap_uid=987654 AND NOT EXISTS(SELECT 1 FROM email_inbound_attachments a WHERE a.inbound_id=email_inbound.id);
SELECT id FROM email_inbound WHERE tenant_id=1 AND imap_uid=987654;
"@
$sqlFile=Join-Path $PSScriptRoot 'mail-fixture.sql';[IO.File]::WriteAllText($sqlFile,$sql,[Text.UTF8Encoding]::new($false))
docker cp $sqlFile erp-d1-20260906-postgres-1:/tmp/d1-mail-fixture.sql
docker exec erp-d1-20260906-postgres-1 psql -U erp_mail -d erp_mail -v ON_ERROR_STOP=1 -f /tmp/d1-mail-fixture.sql
$mailId=(docker exec erp-d1-20260906-postgres-1 psql -U erp_mail -d erp_mail -At -c 'SELECT id FROM email_inbound WHERE tenant_id=1 AND imap_uid=987654').Trim()
$token='d1-mail-acceptance-isolated-fixture'
foreach($id in @('2','3')){docker exec erp-d1-20260906-redis-1 redis-cli SET "erp.mailunlock.t1.e$id.$token" 1 EX 3600|Out-Null}
function MailApi($session,$body){
 $h=@{'X-Mail-Unlock'=$token};foreach($c in $session.Cookies.GetCookies([uri]$base)){if($c.Name -eq 'erp_csrf'){$h['X-CSRF-Token']=$c.Value}}
 (Invoke-RestMethod "$base/api/sourcing-cases" -Method Post -WebSession $session -Headers $h -ContentType 'application/json' -Body ($body|ConvertTo-Json -Depth 20)).data
}
$body=@{sourceMailId=$mailId;customerName='D1 mail customer';lines=@(@{product='Steel';quantity='3';quantityUnit='MT';port='Hamburg';delivery='October';remarks='From test email'})}
try{MailApi $people.S2.session $body|Out-Null;throw 'Other sales imported source mail'}catch [Microsoft.PowerShell.Commands.HttpResponseException]{if([int]$_.Exception.Response.StatusCode -notin @(403,404)){throw}}
$created=(MailApi $s $body).sourcingCase
$r=(Api $s POST '/inquiry-workspace' @{action='get';view='SALES';id=$created.id}).item
if($r.ownerId -ne '2' -or $r.body.contact -ne 'D1 Mail Contact' -or $r.body.attachments.Count -ne 1 -or $r.body.destinationPort -ne 'Hamburg'){throw 'Mail source fields mismatch'}
"A02 actual mail=$mailId -> inquiry=$($r.id), owner=S1, contact/port and original attachment bundle preserved; S2 rejected"

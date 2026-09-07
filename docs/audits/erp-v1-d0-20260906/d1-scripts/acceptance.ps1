param([switch]$SeedOnly)
$ErrorActionPreference='Stop'
& (Join-Path $PSScriptRoot '../../../../scripts/d1-environment.ps1') -Action Check

$base='http://localhost:28080'
function Login([string]$email,[string]$password){
 $s=[Microsoft.PowerShell.Commands.WebRequestSession]::new()
 $r=Invoke-RestMethod "$base/api/auth/login" -Method Post -ContentType 'application/json' -Body (@{email=$email;password=$password}|ConvertTo-Json) -WebSession $s
 return $s
}
function Api($session,[string]$method,[string]$path,$body){
 $headers=@{};foreach($c in $session.Cookies.GetCookies([uri]$base)){if($c.Name -eq 'erp_csrf'){$headers['X-CSRF-Token']=$c.Value}}
 $args=@{Uri="$base/api$path";Method=$method;WebSession=$session;Headers=$headers;ContentType='application/json'}
 if($null -ne $body){$args.Body=($body|ConvertTo-Json -Depth 40 -Compress)}
 return (Invoke-RestMethod @args).data
}
$admin=Login 'admin@d1.example.test' 'admin123'
$roles=(Api $admin GET '/roles' $null).roles
$people=@{}
foreach($pair in @(@('S1','SALES'),@('S2','SALES'),@('P1','BUYER'),@('P2','BUYER'),@('L1','LOGISTICS'),@('L2','LOGISTICS'),@('M1','SALES_MANAGER'),@('B1','BOSS'),@('U1',''))){
 $name=$pair[0];$email="$($name.ToLower())@d1.example.test"
 $existing=(Api $admin GET "/employees?keyword=$name&size=100" $null).employees|Where-Object {$_.code -eq "D1-$name"}|Select-Object -First 1
 if(!$existing){$existing=(Api $admin POST '/employees' @{code="D1-$name";name=$name;email=$email;departmentId='1';initialPassword='D1-Acceptance-2026!';username=$email}).employee}
 if($pair[1]){$role=$roles|Where-Object {$_.code -eq $pair[1]}|Select-Object -First 1;if(!$role){throw "Missing preset $($pair[1])"};Api $admin POST "/employees/$($existing.id)/roles" @{roleIds=@([string]$role.id)}|Out-Null}
 docker exec erp-d1-20260906-postgres-1 psql -U erp_iam -d erp_iam -c "UPDATE employees SET email_verified_at=now() WHERE tenant_id=1 AND id=$($existing.id) AND email LIKE '%@d1.example.test'" | Out-Null
 $people[$name]=@{id=$existing.id;session=(Login $email 'D1-Acceptance-2026!')}
}
$people.Keys|Sort-Object|ForEach-Object {"D1 actor $_ employee=$($people[$_].id)"}
if($SeedOnly){return}
$s=$people.S1.session;$p=$people.P1.session;$l=$people.L1.session
$body=@{customer='D1 API customer';contact='Test';delivery='October';loadingPort='Shanghai';destinationPort='Hamburg';remark='D1 isolated';products=@(@{product='Steel';specification='Q235 2mm';quantity='10';unit='MT'},@{product='Coil';specification='Q345 3mm';quantity='20';unit='MT'},@{product='Sheet';specification='Q235 5mm';quantity='30';unit='MT'})}
$r=(Api $s POST '/inquiry-workspace' @{action='save';view='SALES';body=$body}).item
"A01 inquiry=$($r.id) state=$($r.state) revision=$($r.revision)"
$r=(Api $s POST '/inquiry-workspace' @{action='submit';view='SALES';id=$r.id;revision=$r.revision}).item
"A03 submitted state=$($r.state)"
foreach($v in @(@($p,'PROCUREMENT'),@($l,'LOGISTICS'),@($s,'QUOTATIONS'))){$received=(Api $v[0] POST '/inquiry-workspace' @{action='get';view=$v[1];id=$r.id}).item;if($received.body.products.Count -ne 3){throw 'Cross-module product mismatch'};"A03 receiver=$($v[1]) products=3"}
$q=@{company='Factory API';currency='USD';prices=@(@{productId=$r.body.products[0].id;price='12.34'})}
$buyer=(Api $p POST '/inquiry-workspace' @{action='quote';view='PROCUREMENT';id=$r.id;revision=$r.revision;quote=$q}).item
$sales=(Api $s POST '/inquiry-workspace' @{action='get';view='QUOTATIONS';id=$r.id}).item
if($sales.quotes.Count -ne 0){throw 'Saved quote leaked'};'A04 saved quote invisible to sales'
$quote=$buyer.quotes[0]
Api $p POST '/inquiry-workspace' @{action='quote';view='PROCUREMENT';id=$r.id;revision=$r.revision;quoteId=$quote.id;quoteVersion=$quote.version;quote=$q;submit=$true}|Out-Null
$sales=(Api $s POST '/inquiry-workspace' @{action='get';view='QUOTATIONS';id=$r.id}).item
if($sales.quotes.Count -ne 1){throw 'Submitted quote missing'};'A05 submitted quote received'
$withdrawn=(Api $s POST '/inquiry-workspace' @{action='withdraw';view='SALES';id=$r.id;revision=$r.revision}).item
if($withdrawn.state -ne 'WITHDRAWN'||$withdrawn.quotes.Count -ne 0){throw 'Withdrawal failed'};'A10 withdrawal real export RPC succeeded'
'Partial real IAM/gateway/procurement/export chain passed; additional acceptance cases run separately.'

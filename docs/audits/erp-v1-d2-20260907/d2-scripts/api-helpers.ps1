param([switch]$SeedOnly)
$ErrorActionPreference='Stop'
& (Join-Path $PSScriptRoot '../../../../scripts/d2-environment.ps1') -Action Check

$base='http://localhost:28081'
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
